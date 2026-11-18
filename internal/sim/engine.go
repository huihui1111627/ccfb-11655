// Package sim implements the discrete-event LoRa network engine: duty-cycle
// gated transmissions, co-channel interference with capture and quasi-orthogonal
// spreading factors, Class A receive windows, gateway demodulation limits,
// server-side ADR, node-side ADR recovery and battery-life estimation.
package sim

import (
	"container/heap"

	"lorasim/internal/phy"
	"lorasim/internal/scenario"
)

// Mode selects between a fixed-SF deployment and adaptive data rate.
type Mode int

const (
	ModeFixed Mode = iota // every node keeps its manually assigned SF
	ModeADR               // server runs adaptive data rate
)

func (m Mode) String() string {
	if m == ModeFixed {
		return "fixed"
	}
	return "adr"
}

// Config holds every tunable parameter of one simulation run.
type Config struct {
	Mode Mode

	// Radio
	BWHz          int
	RX2BWHz       int
	CRCode        int
	Preamble      int
	AutoLDRO      bool // force LDRO on whenever phy.LDRONeeded is true
	CaptureMargin float64
	CrossSFSIRDB  float64 // SIR at which an inter-SF interferer corrupts
	RX1DelayMS    int64
	RX2DelayMS    int64

	// Gateway
	DemodPerSF     map[phy.SF]int
	DemodTotalCap  int
	GatewayTxPower int

	// Regulatory / MAC
	ADRHistoryFrames int
	ADRTargetMargin  float64
	ADRCmdEvery      int // frames without downlink before server sends ADR/ACK
	NodeFailLimit    int // frames w/o downlink before node first lowers its rate
	NodeFailStep     int // further frames w/o downlink between SF increases

	// Battery
	BatteryMAh        float64
	SleepCurrentUA    float64
	TxCurrentMA       map[int]float64
	RxCurrentMA       float64
	MCUEnergyPerPktMA float64 // mAh spent on sensing + processing per packet

	// Simulation horizon
	DurationSec int64
}

// DefaultConfig returns the validated baseline parameters.
func DefaultConfig(mode Mode) Config {
	return Config{
		Mode:              mode,
		BWHz:              125000,
		RX2BWHz:           125000,
		CRCode:            1, // 4/5
		Preamble:          8,
		AutoLDRO:          true,
		CaptureMargin:     3.0,
		CrossSFSIRDB:      -6.0,
		RX1DelayMS:        1000,
		RX2DelayMS:        2000,
		DemodPerSF:        map[phy.SF]int{phy.SF7: 2, phy.SF8: 1, phy.SF9: 1, phy.SF10: 1, phy.SF11: 1, phy.SF12: 1},
		DemodTotalCap:     8,
		GatewayTxPower:    27,
		ADRHistoryFrames:  20,
		ADRTargetMargin:   6.0,
		ADRCmdEvery:       16,
		NodeFailLimit:     48,
		NodeFailStep:      24,
		BatteryMAh:        8000,
		SleepCurrentUA:    2.5,
		TxCurrentMA:       map[int]float64{2: 29, 5: 44, 8: 63, 11: 86, 14: 105},
		RxCurrentMA:       10.5,
		MCUEnergyPerPktMA: 0.05,
		DurationSec:       6 * 3600,
	}
}

// Event kinds processed by the engine.
const (
	evULStart = iota
	evULEnd
	evDLStart
	evDLEnd
	evRXStart
	evRXEnd
)

// event is one entry in the discrete-event queue.
type event struct {
	time int64 // nanoseconds
	kind int
	seq  int64 // tie-breaker for stable, deterministic ordering
	ref  int   // packet id (or node id for RX-only events)
	win  int   // receive window number for evRXStart/evRXEnd
}

type eventHeap []event

func (h eventHeap) Len() int { return len(h) }
func (h eventHeap) Less(i, j int) bool {
	if h[i].time != h[j].time {
		return h[i].time < h[j].time
	}
	return h[i].seq < h[j].seq
}
func (h eventHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *eventHeap) Push(x any)   { *h = append(*h, x.(event)) }
func (h *eventHeap) Pop() any {
	old := *h
	n := len(old)
	it := old[n-1]
	*h = old[:n-1]
	return it
}

var _ heap.Interface = (*eventHeap)(nil)

// Engine runs one scenario under one Mode. All state lives on the Engine so
// the same binary can run fixed and ADR back to back deterministically.
type Engine struct {
	cfg  Config
	scn  *scenario.Scenario
	band *phy.Band

	queue  eventHeap
	seqGen int64
	now    int64
	endNS  int64

	nodes   []*nodeState
	packets map[int]*Packet
	nextPkt int

	// demodulator occupancy while packets are in the air
	usedPerSF map[phy.SF]int
	usedTotal int

	// gateway half-duplex transmitter
	gwTxBusyUntil int64

	// node per-sub-band availability (duty-cycle closed time)
	nodeDC map[int]map[int]int64
	// gateway per-sub-band availability for downlinks
	gwDC map[int]int64

	// channel timeline: airtime per fixed bucket
	bucketSec int64
	timeline  map[int]map[int]float64 // channelID -> bucket -> occupied seconds

	// aggregate counters
	sfDist      map[phy.SF]int
	lossReasons map[string]int
	dlLoss      map[string]int
	ulAttempted int
	ulSent      int
	ulAcked     int
	ulDroppedDC int

	report *Report
}

// Run executes the full simulation and returns the collected report.
func Run(scn *scenario.Scenario, cfg Config) *Report {
	e := newEngine(scn, cfg)
	e.run()
	return e.buildReport()
}

func newEngine(scn *scenario.Scenario, cfg Config) *Engine {
	band := phy.EU868()
	e := &Engine{
		cfg:           cfg,
		scn:           scn,
		band:          band,
		packets:       map[int]*Packet{},
		usedPerSF:     map[phy.SF]int{},
		nodeDC:        map[int]map[int]int64{},
		gwDC:          map[int]int64{},
		bucketSec:     60 * int64(1e9), // one-minute timeline buckets
		timeline:      map[int]map[int]float64{},
		sfDist:        map[phy.SF]int{},
		lossReasons:   map[string]int{},
		dlLoss:        map[string]int{},
		gwTxBusyUntil: -1,
	}
	for _, spec := range scn.Nodes {
		e.nodes = append(e.nodes, newNodeState(spec, cfg))
	}
	heap.Init(&e.queue)
	return e
}
