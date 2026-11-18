package sim

import (
	"math"
	"math/rand/v2"

	"lorasim/internal/phy"
	"lorasim/internal/scenario"
)

// Direction marks a packet as an uplink or a downlink frame.
type Direction int

const (
	DirUL Direction = iota
	DirDL
)

// Packet is one radio frame in the air or awaiting its receive window.
type Packet struct {
	ID          int
	NodeID      int
	Dir         Direction
	ChannelID   int
	FreqHz      int64
	SF          phy.SF
	BWHz        int
	TxPowerDBm  int
	RSSIDBm     float64 // power arriving at the receiver
	PayloadSize int
	StartNS     int64
	EndNS       int64
	AirNS       int64

	// uplink bookkeeping
	NodeDemodFull bool // gateway had no demodulator path available
	Received      bool // evaluated at UL end
	LossReason    string

	// downlink bookkeeping
	RXWindow int // 1 or 2
	IsADR    bool
	ADRSF    phy.SF
	ADRPower int
	Ack      bool
	DLLost   string // set when the downlink itself is destroyed
}

// nodeState is the mutable per-node state of one run.
type nodeState struct {
	spec scenario.NodeSpec
	rng  *rand.Rand

	sf        phy.SF
	powerDBm  int
	channelID int

	framesSinceDL int // consecutive uplinks not answered by a downlink
	nextULNS      int64
	rx1Done       bool // a successful RX1 suppresses RX2

	// pending downlink targeting this node (one in flight at most)
	pendingDL  int // packet id, or -1
	pendingWin int // window the pending downlink occupies
	rx1DwellNS int64
	rx2DwellNS int64

	// battery
	mAhUsed      float64
	lastEventNS  int64
	stateStartNS int64
	lastState    byte // 'S' sleep, 'T' tx, 'R' rx

	// stats
	ulAttempted int
	ulSent      int
	ulReceived  int
	ulAcked     int
	dcBlocked   int
	lossReasons map[string]int
	sfFrames    map[phy.SF]int

	// ADR margin history (server side, but kept per node)
	marginHist []float64
}

func newNodeState(spec scenario.NodeSpec, cfg Config) *nodeState {
	n := &nodeState{
		spec:        spec,
		rng:         rand.New(rand.NewPCG(uint64(spec.Seed), uint64(spec.Seed^0x5deece66d))),
		sf:          spec.StartSF,
		powerDBm:    spec.StartPowerDBm,
		channelID:   int(spec.ID % phy.EU868().UplinkChannelCount()),
		nextULNS:    -1,
		pendingDL:   -1,
		lossReasons: map[string]int{},
		sfFrames:    map[phy.SF]int{},
	}
	return n
}

// radioCfg builds the physical-layer config for a frame from this node.
func (n *nodeState) radioCfg(cfg Config, sf phy.SF, bwHz int) phy.RadioCfg {
	ldro := cfg.AutoLDRO && phy.LDRONeeded(sf, bwHz)
	return phy.RadioCfg{
		SF:              sf,
		BWHz:            bwHz,
		CRCode:          cfg.CRCode,
		PreambleSymbols: cfg.Preamble,
		LDRO:            ldro,
		ExplicitHeader:  true,
		CRC:             true,
		TxPowerDBm:      n.powerDBm,
	}
}

// closeState charges energy for time spent in the current state up to now.
func (e *Engine) closeState(n *nodeState, nowNS int64) {
	if n.stateStartNS < 0 || nowNS <= n.stateStartNS {
		n.stateStartNS = nowNS
		return
	}
	hours := float64(nowNS-n.stateStartNS) / 3.6e12
	switch n.lastState {
	case 'S':
		n.mAhUsed += (e.cfg.SleepCurrentUA / 1000.0) * hours
	case 'T':
		n.mAhUsed += e.cfg.TxCurrentMA[n.powerDBm] * hours
	case 'R':
		n.mAhUsed += e.cfg.RxCurrentMA * hours
	}
	n.stateStartNS = nowNS
}

// enterState switches the node's power state, closing the previous interval.
func (e *Engine) enterState(n *nodeState, nowNS int64, state byte) {
	e.closeState(n, nowNS)
	n.lastState = state
	n.stateStartNS = nowNS
}

// batteryLifeDays extrapolates measured energy consumption to battery death.
func (e *Engine) batteryLifeDays(n *nodeState) float64 {
	elapsedH := float64(e.now) / 3.6e12
	if elapsedH <= 0 || n.mAhUsed <= 0 {
		return math.NaN()
	}
	rate := n.mAhUsed / elapsedH // mAh per hour
	return e.cfg.BatteryMAh / rate / 24.0
}
