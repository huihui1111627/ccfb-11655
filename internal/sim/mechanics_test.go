package sim

import (
	"testing"

	"lorasim/internal/phy"
)

// TestDutyCycleClosedTime verifies the regulatory closed-time arithmetic used
// by the engine: after a transmission, the sub-band stays unavailable for
// ToA * (1/D - 1).
func TestDutyCycleClosedTime(t *testing.T) {
	band := phy.EU868()
	cases := []struct {
		sbID       int
		airMS      float64
		wantClosed float64 // ms
	}{
		{0, 100, 9900},  // 1 % band: 99x air time
		{1, 100, 99900}, // 0.1 % band: 999x
		{2, 100, 99900}, // 0.1 % band
		{3, 100, 900},   // 10 % RX2 band: 9x
	}
	for _, tc := range cases {
		dc := band.SubBandByID(tc.sbID).DutyCycle
		closed := tc.airMS * (1.0/dc - 1.0)
		if diff := closed - tc.wantClosed; diff > 1 || diff < -1 {
			t.Fatalf("sub-band %d closed time = %.1f ms, want %.1f", tc.sbID, closed, tc.wantClosed)
		}
	}
}

// TestNodeDutyCycleBlocking builds a single-node scenario and confirms the
// accounting identity attempts = sent + duty-cycle-blocked, and that blocked
// frames never enter the over-the-air loss histogram.
func TestNodeDutyCycleBlocking(t *testing.T) {
	scn := buildTinyScenario(1)
	cfg := DefaultConfig(ModeFixed)
	cfg.DurationSec = 2 * 3600
	r := Run(scn, cfg)
	if r.Agg.ULAttempted == 0 {
		t.Fatal("no uplinks attempted")
	}
	if got := r.Agg.DCBlocked + r.Agg.ULSent; got != r.Agg.ULAttempted {
		t.Fatalf("attempts=%d but blocked=%d sent=%d", r.Agg.ULAttempted, r.Agg.DCBlocked, r.Agg.ULSent)
	}
	if _, present := r.Agg.LossReasons[LossDutyCycle]; present {
		t.Fatal("duty-cycle gating must not be counted as an over-the-air loss")
	}
}

// TestDenseDutyCyclePressure drives enough nodes that some frames are gated by
// the regulatory closed time, proving the blocking path actually executes.
func TestDenseDutyCyclePressure(t *testing.T) {
	scn := buildTinyScenario(600)
	cfg := DefaultConfig(ModeFixed)
	cfg.DurationSec = 6 * 3600
	r := Run(scn, cfg)
	if r.Agg.DCBlocked == 0 {
		t.Fatal("expected duty-cycle blocking under a dense 600-node load")
	}
	if r.Agg.ULAttempted != r.Agg.ULSent+r.Agg.DCBlocked {
		t.Fatal("attempts/sent/blocked accounting identity broken")
	}
}

// TestCaptureAndQuasiOrthogonal drives two controlled colliding frames
// through evaluateReception to exercise the capture and inter-SF rules.
func TestCaptureAndQuasiOrthogonal(t *testing.T) {
	e := newEngine(buildTinyScenario(4), DefaultConfig(ModeFixed))

	mk := func(id int, sf phy.SF, ch int, rssi float64, start, end int64) *Packet {
		return &Packet{ID: id, NodeID: id, Dir: DirUL, ChannelID: ch, SF: sf,
			BWHz: 125000, RSSIDBm: rssi, StartNS: start, EndNS: end}
	}

	// 1) same SF, strong winner captures: weak frame loses, strong survives.
	strong := mk(0, phy.SF8, 0, -100, 0, 100)
	weak := mk(1, phy.SF8, 0, -110, 10, 90)
	e.packets = map[int]*Packet{0: strong, 1: weak}
	if reason := e.evaluateReception(strong); reason != "" {
		t.Fatalf("strong packet should capture, got loss %s", reason)
	}
	if weak.LossReason != LossCaptureLost {
		t.Fatalf("weak packet loss = %q, want capture_lost", weak.LossReason)
	}

	// 2) same SF, near-equal powers: neither meets the capture margin.
	a := mk(2, phy.SF9, 1, -100, 0, 100)
	b := mk(3, phy.SF9, 1, -101, 10, 90)
	e.packets = map[int]*Packet{2: a, 3: b}
	if reason := e.evaluateReception(a); reason != LossCaptureLost {
		t.Fatalf("near-equal collision = %q, want capture_lost", reason)
	}

	// 3) different SF, weak interferer below the quasi-orthogonal threshold:
	// the wanted frame still decodes.
	victim := mk(0, phy.SF7, 2, -100, 0, 100)
	quiet := mk(1, phy.SF10, 2, -102, 10, 90) // SIR = +2 dB > -6 dB threshold
	e.packets = map[int]*Packet{0: victim, 1: quiet}
	if reason := e.evaluateReception(victim); reason != "" {
		t.Fatalf("quasi-orthogonal frame wrongly lost: %s", reason)
	}

	// 4) different SF, dominant interferer breaks quasi-orthogonality.
	victim2 := mk(0, phy.SF7, 3, -100, 0, 100)
	loud := mk(1, phy.SF10, 3, -90, 10, 90) // SIR = -10 dB < -6 dB
	e.packets = map[int]*Packet{0: victim2, 1: loud}
	if reason := e.evaluateReception(victim2); reason != LossInterSF {
		t.Fatalf("dominant inter-SF interferer = %q, want inter_sf_interference", reason)
	}
}

// TestDemodulatorCapacity exceeds the per-SF path count and checks the
// demodulator_full loss reason is recorded.
func TestDemodulatorCapacity(t *testing.T) {
	cfg := DefaultConfig(ModeFixed)
	e := newEngine(buildTinyScenario(12), cfg)
	// occupy all SF7 paths and the total ceiling with already-granted frames
	for i := 0; i < cfg.DemodPerSF[phy.SF7]; i++ {
		e.usedPerSF[phy.SF7]++
		e.usedTotal++
	}
	pkt := &Packet{ID: 99, NodeID: 0, Dir: DirUL, ChannelID: 0, SF: phy.SF7,
		BWHz: 125000, RSSIDBm: -60, StartNS: 0, EndNS: 100, NodeDemodFull: true}
	e.packets[99] = pkt
	if reason := e.evaluateReception(pkt); reason != LossDemodFull {
		t.Fatalf("capacity overflow = %q, want demodulator_full", reason)
	}
}

// TestBelowSensitivity forces a frame under the noise-limited floor.
func TestBelowSensitivity(t *testing.T) {
	e := newEngine(buildTinyScenario(1), DefaultConfig(ModeFixed))
	pkt := &Packet{ID: 0, NodeID: 0, Dir: DirUL, ChannelID: 0, SF: phy.SF7,
		BWHz: 125000, RSSIDBm: -140, StartNS: 0, EndNS: 100}
	e.packets[0] = pkt
	if reason := e.evaluateReception(pkt); reason != LossBelowNoise {
		t.Fatalf("weak frame = %q, want below_sensitivity", reason)
	}
}
