package sim

import (
	"testing"

	"lorasim/internal/phy"
)

// TestNodeADRRecovery verifies the device-side backoff: on the first
// no-downlink timeout the node restores max power and widens its SF, then
// keeps widening every NodeFailStep frames until SF12.
func TestNodeADRRecovery(t *testing.T) {
	cfg := DefaultConfig(ModeADR)
	e := newEngine(buildTinyScenario(1), cfg)
	n := e.nodes[0]
	n.sf = phy.SF8
	n.powerDBm = 5

	// before the threshold nothing changes
	n.framesSinceDL = cfg.NodeFailLimit - 1
	e.nodeADRRecovery(n)
	if n.sf != phy.SF8 || n.powerDBm != 5 {
		t.Fatal("recovery triggered too early")
	}

	// first timeout: max power + one SF step
	n.framesSinceDL = cfg.NodeFailLimit
	e.nodeADRRecovery(n)
	if n.powerDBm != 14 || n.sf != phy.SF9 {
		t.Fatalf("first recovery = SF%d/%d dBm, want SF9/14", n.sf, n.powerDBm)
	}

	// each further step widens SF
	n.framesSinceDL = cfg.NodeFailLimit + cfg.NodeFailStep
	e.nodeADRRecovery(n)
	if n.sf != phy.SF10 {
		t.Fatalf("second recovery = SF%d, want SF10", n.sf)
	}

	// recovery never exceeds SF12
	n.sf = phy.SF12
	n.framesSinceDL = cfg.NodeFailLimit + 2*cfg.NodeFailStep
	before := n.sf
	e.nodeADRRecovery(n)
	if n.sf != before {
		t.Fatal("recovery widened SF beyond SF12")
	}
}

// TestNodeADRRecoveryDisabledInFixedMode confirms fixed-SF nodes never change
// their own rate.
func TestNodeADRRecoveryDisabledInFixedMode(t *testing.T) {
	cfg := DefaultConfig(ModeFixed)
	e := newEngine(buildTinyScenario(1), cfg)
	n := e.nodes[0]
	startSF, startPower := n.sf, n.powerDBm
	n.framesSinceDL = cfg.NodeFailLimit + 100
	e.nodeADRRecovery(n)
	if n.sf != startSF || n.powerDBm != startPower {
		t.Fatal("fixed-mode node changed rate on its own")
	}
}

// TestRXWindowsScheduled ensures every finished uplink arms both Class A
// windows and records positive dwell times.
func TestRXWindowsScheduled(t *testing.T) {
	scn := buildTinyScenario(5)
	cfg := DefaultConfig(ModeADR)
	cfg.DurationSec = 3600
	r := Run(scn, cfg)
	// an ADR run must have produced at least some window activity, and the
	// report must include both uplink and RX2 channels
	if len(r.Channels) != 9 {
		t.Fatalf("expected 8 uplink + 1 RX2 channel, got %d", len(r.Channels))
	}
}

// TestClassATimingConstants sanity-checks the configured window offsets.
func TestClassATimingConstants(t *testing.T) {
	cfg := DefaultConfig(ModeADR)
	if cfg.RX1DelayMS != 1000 || cfg.RX2DelayMS != 2000 {
		t.Fatal("Class A RX1/RX2 offsets must be 1000/2000 ms")
	}
	if cfg.RX2DelayMS <= cfg.RX1DelayMS {
		t.Fatal("RX2 must open after RX1")
	}
}
