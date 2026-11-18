package sim

import (
	"encoding/json"
	"testing"

	"lorasim/internal/scenario"
)

// runBoth executes fixed and ADR runs for a small deterministic scenario.
func runBoth(t *testing.T, seed int64, nodes int, hours int) (*Report, *Report) {
	t.Helper()
	scfg := scenario.DefaultConfig(seed)
	scfg.NodeCount = nodes
	scn := scenario.Generate(scfg)

	fcfg := DefaultConfig(ModeFixed)
	fcfg.DurationSec = int64(hours) * 3600
	acfg := DefaultConfig(ModeADR)
	acfg.DurationSec = int64(hours) * 3600

	return Run(scn, fcfg), Run(scn, acfg)
}

// TestDeterminism is the core reproducibility requirement: the same seed and
// configuration must produce byte-identical JSON reports across independent
// runs and independent Engine instances.
func TestDeterminism(t *testing.T) {
	for _, mode := range []Mode{ModeFixed, ModeADR} {
		scn1 := scenario.Generate(func() scenario.Config {
			c := scenario.DefaultConfig(777)
			c.NodeCount = 60
			return c
		}())
		scn2 := scenario.Generate(func() scenario.Config {
			c := scenario.DefaultConfig(777)
			c.NodeCount = 60
			return c
		}())
		cfg := DefaultConfig(mode)
		cfg.DurationSec = 3 * 3600

		r1 := Run(scn1, cfg)
		r2 := Run(scn2, cfg)
		b1, _ := json.Marshal(r1)
		b2, _ := json.Marshal(r2)
		if string(b1) != string(b2) {
			t.Fatalf("%s: repeated runs with seed 777 diverged", mode)
		}
		if r1.ConfigHash == "" || r1.ConfigHash != r2.ConfigHash {
			t.Fatalf("%s: config fingerprint mismatch", mode)
		}
	}
}

// TestDifferentSeedsDiffer guards against an accidentally constant RNG:
// distinct seeds must yield distinct deployments and reports.
func TestDifferentSeedsDiffer(t *testing.T) {
	a, _ := runBoth(t, 1, 40, 2)
	b, _ := runBoth(t, 2, 40, 2)
	if a.Seed == b.Seed {
		t.Fatal("seeds not propagated")
	}
	ba, _ := json.Marshal(a.Nodes[0])
	bb, _ := json.Marshal(b.Nodes[0])
	if string(ba) == string(bb) {
		t.Fatal("node 0 identical across different seeds")
	}
}

// TestFixedModeKeepsSF asserts that a fixed-SF run never changes a node's
// spreading factor or transmit power and never sends a downlink.
func TestFixedModeKeepsSF(t *testing.T) {
	fixed, _ := runBoth(t, 42, 50, 4)
	for _, nr := range fixed.Nodes {
		if nr.ULAcked != 0 {
			t.Fatalf("node %d received a downlink in fixed mode", nr.ID)
		}
	}
}

// TestADRConvergesDown asserts that, given healthy links, ADR eventually
// moves a meaningful share of frames onto the faster SF7..SF9 rates.
func TestADRConvergesDown(t *testing.T) {
	_, adr := runBoth(t, 42, 120, 12)
	fast := adr.Agg.SFDistribution["SF7"] + adr.Agg.SFDistribution["SF8"] +
		adr.Agg.SFDistribution["SF9"]
	total := 0
	for _, c := range adr.Agg.SFDistribution {
		total += c
	}
	if total == 0 || float64(fast)/float64(total) < 0.35 {
		t.Fatalf("ADR did not converge toward fast rates: %d/%d frames", fast, total)
	}
	if adr.Agg.ULAcked == 0 {
		t.Fatal("ADR run delivered no downlinks")
	}
}

// TestDeliveryRateSane bounds the headline metric and per-node accounting.
func TestDeliveryRateSane(t *testing.T) {
	fixed, adr := runBoth(t, 99, 150, 6)
	for _, r := range []*Report{fixed, adr} {
		if r.Agg.NetworkDeliveryRate < 0 || r.Agg.NetworkDeliveryRate > 1.0000001 {
			t.Fatalf("%s delivery rate out of range: %f", r.Agg.Mode, r.Agg.NetworkDeliveryRate)
		}
		if r.Agg.ULReceived > r.Agg.ULSent {
			t.Fatalf("%s received more than sent", r.Agg.Mode)
		}
		for _, n := range r.Nodes {
			if n.ULReceived > n.ULSent || n.ULSent > n.ULAttempted {
				t.Fatalf("node %d counters inconsistent", n.ID)
			}
			if n.BatteryLifeDays <= 0 {
				t.Fatalf("node %d non-positive battery life", n.ID)
			}
		}
	}
}
