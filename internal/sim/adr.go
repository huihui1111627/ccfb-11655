package sim

import "lorasim/internal/phy"

// recordMargin stores the SNR margin of a successfully received uplink in the
// server's per-node rolling history (most recent ADRHistoryFrames frames).
func (e *Engine) recordMargin(n *nodeState, pkt *Packet) {
	snr := pkt.RSSIDBm - phy.NoiseFloor(pkt.BWHz)
	margin := snr - phy.RequiredSNR(pkt.SF)
	n.marginHist = append(n.marginHist, margin)
	if len(n.marginHist) > e.cfg.ADRHistoryFrames {
		n.marginHist = n.marginHist[1:]
	}
}

// serverADRDecision implements the network-server adaptive data rate loop
// following the LoRaWAN ADR procedure (RP002, NS-adaptive-data-rate):
//
//   - the link margin is the WORST (minimum) margin observed over the most
//     recent 20 frames, not the average — a command must survive deep fades;
//   - positive margin first lowers the spreading factor (air-time/capacity
//     benefit) and only then reduces TX power (battery benefit), each step
//     worth 3 dB;
//   - negative margin first raises TX power back toward maximum and only then
//     widens the spreading factor, matching device reachability priorities;
//   - even with no rate change, a keep-alive downlink is sent every
//     ADRCmdEvery frames so the device's ADR-failure counter keeps resetting.
func (e *Engine) serverADRDecision(n *nodeState, pkt *Packet) (wantDL bool, newSF phy.SF, newPower int) {
	e.recordMargin(n, pkt)

	newSF, newPower = n.sf, n.powerDBm
	if n.framesSinceDL >= e.cfg.ADRCmdEvery {
		wantDL = true
	}
	if len(n.marginHist) < e.cfg.ADRHistoryFrames {
		return wantDL, newSF, newPower
	}

	worst := n.marginHist[0]
	for _, m := range n.marginHist[1:] {
		if m < worst {
			worst = m
		}
	}
	budgetDB := worst - e.cfg.ADRTargetMargin
	steps := int(budgetDB / 3.0) // truncate toward zero
	if steps == 0 {
		return wantDL, newSF, newPower
	}

	wantDL = true
	if steps > 0 {
		// spare link budget: narrow SF first for shorter air time, then
		// reduce transmit power once the fastest data rate is reached
		for steps > 0 {
			if newSF > phy.SF7 {
				newSF--
				steps--
				continue
			}
			if newPower > 2 {
				newPower -= 3
				if newPower < 2 {
					newPower = 2
				}
				steps--
				continue
			}
			break
		}
	} else {
		// deficit: restore TX power first, then widen the spreading factor
		for steps < 0 {
			if newPower < 14 {
				newPower += 3
				if newPower > 14 {
					newPower = 14
				}
				steps++
				continue
			}
			if newSF < phy.SF12 {
				newSF++
				steps++
				continue
			}
			break
		}
	}
	return wantDL, newSF, newPower
}

// nodeADRRecovery implements the device-side LoRaWAN ADR-ADAPT behavior:
// after a long period without any downlink, the node first restores maximum
// TX power and then widens its spreading factor step by step until it reaches
// SF12, so a node whose link has degraded can rejoin the network.
func (e *Engine) nodeADRRecovery(n *nodeState) {
	if e.cfg.Mode != ModeADR {
		return
	}
	f := n.framesSinceDL
	switch {
	case f == e.cfg.NodeFailLimit:
		// first timeout: max power and one SF step up
		n.powerDBm = 14
		if n.sf < phy.SF12 {
			n.sf++
		}
	case f > e.cfg.NodeFailLimit && (f-e.cfg.NodeFailLimit)%e.cfg.NodeFailStep == 0:
		// every further NodeFailStep frames: another SF step up
		if n.sf < phy.SF12 {
			n.sf++
		}
	}
}
