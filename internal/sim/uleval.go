package sim

import "lorasim/internal/phy"

// handleULEnd evaluates reception once the frame finishes, frees the
// demodulator, opens the Class A windows and schedules the next uplink.
func (e *Engine) handleULEnd(pktID int) {
	pkt, ok := e.packets[pktID]
	if !ok {
		return
	}
	n := e.nodes[pkt.NodeID]
	delete(e.packets, pktID)

	// free the demodulator path if one was granted
	if !pkt.NodeDemodFull {
		e.usedPerSF[pkt.SF]--
		e.usedTotal--
	}

	reason := e.evaluateReception(pkt)
	if reason != "" {
		pkt.Received = false
		if pkt.LossReason == "" { // may already be pre-marked by a capture
			pkt.LossReason = reason
		}
		n.lossReasons[pkt.LossReason]++
		e.lossReasons[pkt.LossReason]++
	} else {
		pkt.Received = true
		n.ulReceived++
		e.ulAckedPlaceholder(pkt)
	}
	n.ulSent++
	e.ulSent++

	// MCU sensing/processing energy per attempted frame
	n.mAhUsed += e.cfg.MCUEnergyPerPktMA
	e.enterState(n, pkt.EndNS, 'S')

	// Class A: RX1 and RX2 open relative to end of uplink regardless of
	// whether the server schedules a downlink (the node always listens).
	e.openRXWindows(pkt)

	// schedule next periodic uplink; duty cycle is re-checked at start
	periodNS := int64(n.spec.ReportPeriodSec) * int64(1e9)
	next := pkt.EndNS + periodNS
	n.nextULNS = next
	e.schedule(next, evULStart, n.spec.ID, 0)
}

// pktNode resolves the owning node state for a packet.
func (e *Engine) pktNode(pkt *Packet) *nodeState { return e.nodes[pkt.NodeID] }

// evaluateReception returns "" on successful demodulation or the loss reason.
//
// Rules implemented:
//  1. no demodulator path available at arrival -> demodulator_full;
//  2. signal under the noise-limited sensitivity -> below_sensitivity;
//  3. same-SF overlap: the stronger packet is captured when its SIR exceeds
//     CaptureMargin, otherwise both frames are lost (capture_lost);
//  4. different-SF overlap is quasi-orthogonal and only corrupts the frame
//     when the interferer is strong enough to beat CrossSFSIRDB.
func (e *Engine) evaluateReception(pkt *Packet) string {
	if pkt.LossReason != "" { // a stronger packet already won the capture
		return pkt.LossReason
	}
	if pkt.NodeDemodFull {
		return LossDemodFull
	}

	noiseFloor := phy.NoiseFloor(pkt.BWHz)
	snr := pkt.RSSIDBm - noiseFloor
	if snr < phy.RequiredSNR(pkt.SF) {
		return LossBelowNoise
	}

	var strongestSameSF *Packet
	var sameSFOverlap []*Packet
	for _, other := range e.packets {
		if other.Dir != DirUL || other.ID == pkt.ID || other.ChannelID != pkt.ChannelID {
			continue
		}
		if !overlaps(pkt.StartNS, pkt.EndNS, other.StartNS, other.EndNS) {
			continue
		}
		if other.SF == pkt.SF {
			sameSFOverlap = append(sameSFOverlap, other)
			if strongestSameSF == nil || other.RSSIDBm > strongestSameSF.RSSIDBm {
				strongestSameSF = other
			}
		} else {
			// quasi-orthogonal: interferer must substantially overpower
			sir := pkt.RSSIDBm - other.RSSIDBm
			if sir < e.cfg.CrossSFSIRDB {
				return LossInterSF
			}
		}
	}

	if len(sameSFOverlap) > 0 {
		if strongestSameSF.RSSIDBm > pkt.RSSIDBm {
			// another frame is stronger; it gets the capture, we lose
			return LossCaptureLost
		}
		// we are the strongest: every weaker same-SF collider must sit below
		// the capture margin, otherwise the receiver cannot resolve us.
		for _, w := range sameSFOverlap {
			if pkt.RSSIDBm-w.RSSIDBm < e.cfg.CaptureMargin {
				return LossCaptureLost
			}
		}
		// capture success: pre-mark the weaker colliders as lost so their
		// own end-of-frame evaluation short-circuits. Only the reason is set
		// here; the counters are incremented exactly once when each weak
		// packet's own end-of-frame handler runs.
		for _, w := range sameSFOverlap {
			if w.LossReason == "" {
				w.LossReason = LossCaptureLost
			}
		}
	}
	return ""
}

func overlaps(aStart, aEnd, bStart, bEnd int64) bool {
	return aStart < bEnd && bStart < aEnd
}

// ulAckedPlaceholder keeps uplink accounting in one place; actual ADR/ACK
// decisions run in adr.go when the downlink is built.
func (e *Engine) ulAckedPlaceholder(pkt *Packet) {}
