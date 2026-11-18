package sim

import (
	"container/heap"
	"math"

	"lorasim/internal/phy"
)

// Loss-reason labels reported in the dashboard.
const (
	LossDemodFull   = "demodulator_full"
	LossBelowNoise  = "below_sensitivity"
	LossCaptureLost = "capture_lost"
	LossInterSF     = "inter_sf_interference"
	// LossDutyCycle is tracked via the dcBlocked counters rather than the
	// loss histogram, because a duty-cycle-gated frame is delayed and retried
	// rather than lost over the air.
	LossDutyCycle = "duty_cycle_blocked"
)

// Downlink loss-reason labels.
const (
	DLLossGatewayTX  = "gateway_tx_busy"
	DLLossDutyCycle  = "downlink_duty_cycle"
	LossDLInterfered = "downlink_interfered"
	LossWindowMissed = "rx_window_missed"
)

// run is the deterministic event loop: events at the same instant are ordered
// by insertion sequence, so no wall clock or goroutine scheduling affects the
// result.
func (e *Engine) run() {
	e.endNS = e.cfg.DurationSec * int64(1e9)

	for i := range e.nodes {
		n := e.nodes[i]
		n.nextULNS = e.firstUplinkAt(n)
		n.stateStartNS = 0
		n.lastState = 'S'
		e.schedule(n.nextULNS, evULStart, n.spec.ID, 0)
	}

	for e.queue.Len() > 0 {
		ev := heap.Pop(&e.queue).(event)
		if ev.time > e.endNS {
			break
		}
		e.now = ev.time
		switch ev.kind {
		case evULStart:
			e.handleULStart(ev.ref)
		case evULEnd:
			e.handleULEnd(ev.ref)
		case evDLStart:
			e.handleDLStart(ev.ref)
		case evDLEnd:
			e.handleDLEnd(ev.ref)
		case evRXStart:
			e.handleRXStart(ev.ref, ev.win)
		case evRXEnd:
			e.handleRXEnd(ev.ref, ev.win)
		}
	}

	// close every node's final sleep interval for correct energy totals
	for _, n := range e.nodes {
		e.closeState(n, e.endNS)
	}
}

func (e *Engine) schedule(ns int64, kind, ref, win int) {
	e.seqGen++
	heap.Push(&e.queue, event{time: ns, kind: kind, seq: e.seqGen, ref: ref, win: win})
}

// firstUplinkAt staggers initial transmissions inside one reporting period so
// the network does not switch on all at once at t=0.
func (e *Engine) firstUplinkAt(n *nodeState) int64 {
	jitter := n.rng.Float64() * float64(n.spec.ReportPeriodSec)
	return int64(jitter * 1e9)
}

// handleULStart opens a transmission if regulatory and gateway capacity allow.
func (e *Engine) handleULStart(nodeID int) {
	n := e.nodes[nodeID]

	// Every frame pseudo-randomly picks one of the band's uplink channels so
	// load spreads across sub-bands; the draw comes from the node's private
	// deterministic RNG stream.
	n.channelID = int(n.rng.Uint64() % uint64(e.band.UplinkChannelCount()))
	ch := e.band.Channels[n.channelID]
	sb := e.band.SubBandOfChannel(ch.ID)

	// Regulatory duty cycle: the node must have waited the closed time for
	// every prior transmission on this sub-band. A blocked frame is not an
	// over-the-air loss — it is retried when the closed time elapses — so it
	// counts as an attempted frame and a separate dcBlocked metric, never in
	// the lossReasons histogram.
	if avail := e.nodeAvailableAt(nodeID, sb.ID); avail > e.now {
		n.dcBlocked++
		n.ulAttempted++
		e.ulDroppedDC++
		e.ulAttempted++
		// retry as soon as the closed time elapses
		e.schedule(avail, evULStart, nodeID, 0)
		return
	}

	cfg := n.radioCfg(e.cfg, n.sf, e.cfg.BWHz)
	air := phy.AirtimeCalc(n.spec.PayloadBytes, cfg)
	startNS := e.now
	endNS := startNS + air.TotalNS

	// fast fading: independent per-packet Rayleigh-like amplitude variation,
	// drawn from the node's own deterministic RNG stream.
	fast := n.rng.NormFloat64() * 2.0
	pathLoss := e.scn.PathLoss(&n.spec)
	rssi := float64(n.powerDBm) - pathLoss + fast

	pkt := &Packet{
		ID:          e.nextPkt,
		NodeID:      nodeID,
		Dir:         DirUL,
		ChannelID:   ch.ID,
		FreqHz:      ch.FreqHz,
		SF:          n.sf,
		BWHz:        e.cfg.BWHz,
		TxPowerDBm:  n.powerDBm,
		RSSIDBm:     rssi,
		PayloadSize: n.spec.PayloadBytes,
		StartNS:     startNS,
		EndNS:       endNS,
		AirNS:       air.TotalNS,
	}
	e.nextPkt++
	n.ulAttempted++
	e.ulAttempted++

	// Gateway demodulation capacity: one SX130x-style path per SF, a small
	// number of concurrent paths per SF and a total ceiling.
	if e.usedPerSF[n.sf] >= e.cfg.DemodPerSF[n.sf] || e.usedTotal >= e.cfg.DemodTotalCap {
		pkt.NodeDemodFull = true
	} else {
		e.usedPerSF[n.sf]++
		e.usedTotal++
	}

	e.packets[pkt.ID] = pkt
	e.enterState(n, startNS, 'T')
	n.sfFrames[n.sf]++
	e.sfDist[n.sf]++
	e.accountTimeline(ch.ID, startNS, endNS)
	e.markNodeTx(nodeID, sb.ID, startNS, endNS)

	e.schedule(endNS, evULEnd, pkt.ID, 0)
}

// accountTimeline adds occupied air time to the per-channel timeline buckets.
func (e *Engine) accountTimeline(channelID int, startNS, endNS int64) {
	buckets, ok := e.timeline[channelID]
	if !ok {
		buckets = map[int]float64{}
		e.timeline[channelID] = buckets
	}
	b0 := startNS / e.bucketSec
	b1 := endNS / e.bucketSec
	if b0 == b1 {
		buckets[int(b0)] += float64(endNS-startNS) / 1e9
		return
	}
	// split a frame straddling a bucket boundary
	buckets[int(b0)] += float64((b0+1)*e.bucketSec-startNS) / 1e9
	for b := b0 + 1; b < b1; b++ {
		buckets[int(b)] += float64(e.bucketSec) / 1e9
	}
	buckets[int(b1)] += float64(endNS-b1*e.bucketSec) / 1e9
}

// markNodeTx records the duty-cycle closed time of a node transmission.
// Closed time = ToA * (1/D - 1), so the next transmission on that sub-band
// may start no earlier than end + closed time.
func (e *Engine) markNodeTx(nodeID, sbID int, startNS, endNS int64) {
	dc := e.band.SubBandByID(sbID).DutyCycle
	closedNS := int64(math.Round(float64(endNS-startNS) * (1.0/dc - 1.0)))
	availableAt := endNS + closedNS
	if e.nodeDC[nodeID] == nil {
		e.nodeDC[nodeID] = map[int]int64{}
	}
	if availableAt > e.nodeDC[nodeID][sbID] {
		e.nodeDC[nodeID][sbID] = availableAt
	}
}

func (e *Engine) nodeAvailableAt(nodeID, sbID int) int64 {
	if m := e.nodeDC[nodeID]; m != nil {
		return m[sbID]
	}
	return 0
}
