package sim

import (
	"math"

	"lorasim/internal/phy"
)

// openRXWindows schedules the two Class A receive windows relative to the end
// of the uplink, and asks the network server whether it has a downlink.
func (e *Engine) openRXWindows(ul *Packet) {
	n := e.nodes[ul.NodeID]

	rx1Open := ul.EndNS + e.cfg.RX1DelayMS*int64(1e6)
	rx2Open := ul.EndNS + e.cfg.RX2DelayMS*int64(1e6)

	// dwell = preamble symbols + 5 symbol times (capped at 125 ms)
	rx1Cfg := n.radioCfg(e.cfg, ul.SF, e.cfg.BWHz)
	rx2Cfg := n.radioCfg(e.cfg, e.band.RX2SF, e.cfg.RX2BWHz)
	n.rx1DwellNS = e.dwellNS(rx1Cfg)
	n.rx2DwellNS = e.dwellNS(rx2Cfg)

	// Node-side ADR recovery bookkeeping happens once per finished uplink,
	// before the server gets a chance to answer in the windows.
	n.framesSinceDL++
	e.nodeADRRecovery(n)

	if e.cfg.Mode != ModeADR || !ul.Received {
		// fixed-SF mode: no server downlinks; or frame never reached the
		// network server. The node still opens both windows.
		e.schedule(rx1Open, evRXStart, n.spec.ID, 1)
		e.schedule(rx2Open, evRXStart, n.spec.ID, 2)
		return
	}

	// Server-side ADR decision for this received frame.
	wantDL, adrSF, adrPower := e.serverADRDecision(n, ul)
	dlRSSI := float64(e.cfg.GatewayTxPower) - e.scn.PathLoss(&n.spec)

	// RX1 echoes the uplink channel and data rate.
	rx1Ch := e.band.Channels[ul.ChannelID]
	rx1Air := phy.AirtimeCalc(downlinkPayload, rx1Cfg).TotalNS
	if wantDL && e.dlFeasible(rx1Ch.SubBandID, rx1Open, rx1Air, n.rx1DwellNS, rx1Ch.ID, ul.SF, dlRSSI) {
		e.createDownlink(ul, 1, rx1Ch, ul.SF, e.cfg.BWHz, rx1Air, adrSF, adrPower)
		e.schedule(rx1Open, evRXStart, n.spec.ID, 1)
		e.schedule(rx2Open, evRXStart, n.spec.ID, 2)
		return
	}

	// RX2: fixed g3 channel, SF12.
	rx2Ch := e.band.RX2Channel()
	rx2Air := phy.AirtimeCalc(downlinkPayload, rx2Cfg).TotalNS
	if wantDL && e.dlFeasible(rx2Ch.SubBandID, rx2Open, rx2Air, n.rx2DwellNS, rx2Ch.ID, e.band.RX2SF, dlRSSI) {
		e.createDownlink(ul, 2, rx2Ch, e.band.RX2SF, e.cfg.RX2BWHz, rx2Air, adrSF, adrPower)
		e.schedule(rx1Open, evRXStart, n.spec.ID, 1)
		e.schedule(rx2Open, evRXStart, n.spec.ID, 2)
		return
	}

	if wantDL {
		// the server wanted to answer but neither window could be served
		e.dlLoss[LossWindowMissed]++
	}
	e.schedule(rx1Open, evRXStart, n.spec.ID, 1)
	e.schedule(rx2Open, evRXStart, n.spec.ID, 2)
}

const downlinkPayload = 12 // bytes of MAC payload on a downlink ACK/ADR frame

// dwellNS returns how long a node keeps a receive window open.
func (e *Engine) dwellNS(cfg phy.RadioCfg) int64 {
	tsym := phy.SymbolTime(cfg)
	dwell := float64(cfg.PreambleSymbols+5) * tsym
	if dwell > 0.125 {
		dwell = 0.125
	}
	return int64(math.Round(dwell * 1e9))
}

// dlFeasible checks the gateway half-duplex transmitter, the regulatory
// sub-band availability and the in-flight uplink interference for a candidate
// downlink. The frame only has to START inside the receive window: once the
// preamble is detected the node keeps the window open for the whole frame, so
// frame ToA is not bounded by dwell time.
func (e *Engine) dlFeasible(sbID int, openNS, airNS, dwellNS int64, chID int, dlSF phy.SF, dlRSSI float64) bool {
	if openNS < e.gwTxBusyUntil {
		return false
	}
	if e.gwDC[sbID] > openNS {
		return false
	}
	// Reject a downlink that an overlapping uplink would already destroy at
	// the node receiver, so the gateway does not burn transmit air time on a
	// frame that cannot decode. The same capture/quasi-orthogonal model used
	// in evaluateDLReception applies at scheduling time.
	endNS := openNS + airNS
	for _, other := range e.packets {
		if other.Dir != DirUL || other.ChannelID != chID {
			continue
		}
		if !overlaps(openNS, endNS, other.StartNS, other.EndNS) {
			continue
		}
		sir := dlRSSI - other.RSSIDBm
		if other.SF == dlSF {
			if sir < e.cfg.CaptureMargin {
				return false
			}
		} else if sir < e.cfg.CrossSFSIRDB {
			return false
		}
	}
	return true
}

// createDownlink reserves the gateway resources and queues the frame.
func (e *Engine) createDownlink(ul *Packet, win int, ch phy.Channel, sf phy.SF, bwHz int, airNS int64, adrSF phy.SF, adrPower int) {
	n := e.nodes[ul.NodeID]
	openNS := ul.EndNS
	if win == 1 {
		openNS += e.cfg.RX1DelayMS * int64(1e6)
	} else {
		openNS += e.cfg.RX2DelayMS * int64(1e6)
	}
	endNS := openNS + airNS

	pathLoss := e.scn.PathLoss(&n.spec)
	rssi := float64(e.cfg.GatewayTxPower) - pathLoss

	pkt := &Packet{
		ID:          e.nextPkt,
		NodeID:      n.spec.ID,
		Dir:         DirDL,
		ChannelID:   ch.ID,
		FreqHz:      ch.FreqHz,
		SF:          sf,
		BWHz:        bwHz,
		TxPowerDBm:  e.cfg.GatewayTxPower,
		RSSIDBm:     rssi,
		PayloadSize: downlinkPayload,
		StartNS:     openNS,
		EndNS:       endNS,
		AirNS:       airNS,
		RXWindow:    win,
		IsADR:       true,
		ADRSF:       adrSF,
		ADRPower:    adrPower,
		Ack:         true,
	}
	e.nextPkt++
	e.packets[pkt.ID] = pkt

	// reserve the half-duplex transmitter and the sub-band duty cycle
	e.gwTxBusyUntil = endNS
	dc := e.band.SubBandByID(ch.SubBandID).DutyCycle
	closedNS := int64(math.Round(float64(airNS) * (1.0/dc - 1.0)))
	if avail := endNS + closedNS; avail > e.gwDC[ch.SubBandID] {
		e.gwDC[ch.SubBandID] = avail
	}

	n.pendingDL = pkt.ID
	n.pendingWin = win
	e.schedule(openNS, evDLStart, pkt.ID, 0)
}

// handleDLStart re-validates the reservation (all reservations are taken at
// scheduling time, so this mainly records the frame on the timeline).
func (e *Engine) handleDLStart(pktID int) {
	pkt, ok := e.packets[pktID]
	if !ok {
		return
	}
	e.accountTimeline(pkt.ChannelID, pkt.StartNS, pkt.EndNS)
	e.schedule(pkt.EndNS, evDLEnd, pkt.ID, 0)
}

// handleDLEnd evaluates the downlink at the node receiver.
func (e *Engine) handleDLEnd(pktID int) {
	pkt, ok := e.packets[pktID]
	if !ok {
		return
	}
	n := e.nodes[pkt.NodeID]
	delete(e.packets, pktID)

	// Downlink interference from overlapping uplinks on the same channel.
	// Same-SF capture and inter-SF quasi-orthogonality use the same model.
	reason := e.evaluateDLReception(pkt)
	if reason != "" {
		pkt.DLLost = reason
		e.dlLoss[reason]++
		n.pendingDL = -1
		e.enterState(n, pkt.EndNS, 'S')
		return
	}

	// Successful downlink inside the node's window.
	n.framesSinceDL = 0
	n.ulAcked++
	e.ulAcked++
	if pkt.RXWindow == 1 {
		n.rx1Done = true // RX2 stays closed
	}
	if pkt.IsADR {
		n.sf = pkt.ADRSF
		n.powerDBm = pkt.ADRPower
	}
	n.pendingDL = -1
	e.enterState(n, pkt.EndNS, 'S')
}

// evaluateDLReception mirrors the uplink interference model at the node.
func (e *Engine) evaluateDLReception(pkt *Packet) string {
	noiseFloor := phy.NoiseFloor(pkt.BWHz)
	if pkt.RSSIDBm-noiseFloor < phy.RequiredSNR(pkt.SF) {
		return LossBelowNoise
	}
	for _, other := range e.packets {
		if other.Dir != DirUL || other.ChannelID != pkt.ChannelID {
			continue
		}
		if !overlaps(pkt.StartNS, pkt.EndNS, other.StartNS, other.EndNS) {
			continue
		}
		sir := pkt.RSSIDBm - other.RSSIDBm
		if other.SF == pkt.SF {
			if sir < e.cfg.CaptureMargin {
				return LossDLInterfered
			}
		} else if sir < e.cfg.CrossSFSIRDB {
			return LossDLInterfered
		}
	}
	return ""
}

// handleRXStart switches the node into receive state for a window.
func (e *Engine) handleRXStart(nodeID, win int) {
	n := e.nodes[nodeID]
	// a successful RX1 suppresses RX2 entirely
	if win == 2 && n.rx1Done {
		return
	}
	openNS := e.now
	e.enterState(n, openNS, 'R')
	var dwell int64
	if win == 1 {
		dwell = n.rx1DwellNS
	} else {
		dwell = n.rx2DwellNS
	}
	e.schedule(openNS+dwell, evRXEnd, nodeID, win)
}

// handleRXEnd closes a window in which no downlink preamble was detected.
// If a downlink is still in flight the node keeps the receiver on until the
// frame finishes, and handleDLEnd returns it to sleep.
func (e *Engine) handleRXEnd(nodeID, win int) {
	n := e.nodes[nodeID]
	if n.pendingDL >= 0 {
		// a preamble was detected; keep receiving the whole frame
		return
	}
	e.enterState(n, e.now, 'S')
	if win == 2 {
		n.rx1Done = false // reset for the next frame cycle
	}
}
