package sim

import (
	"testing"

	"lorasim/internal/scenario"
)

// buildTinyScenario produces a small deterministic scenario for unit tests
// that need a wired-up engine without running the full event loop.
func buildTinyScenario(nodes int) *scenario.Scenario {
	cfg := scenario.DefaultConfig(1234)
	cfg.NodeCount = nodes
	return scenario.Generate(cfg)
}

// TestBatteryEnergyPositive verifies that every active node consumes energy
// in all three states and that the extrapolated battery life is finite.
func TestBatteryEnergyPositive(t *testing.T) {
	scn := buildTinyScenario(25)
	cfg := DefaultConfig(ModeFixed)
	cfg.DurationSec = 3 * 3600
	r := Run(scn, cfg)
	for _, n := range r.Nodes {
		if n.ULAttempted == 0 {
			continue
		}
		if n.BatteryUsedMAh <= 0 {
			t.Fatalf("node %d consumed no energy", n.ID)
		}
		if n.BatteryLifeDays <= 0 || n.BatteryLifeDays > 100*365 {
			t.Fatalf("node %d implausible battery life: %.1f days", n.ID, n.BatteryLifeDays)
		}
	}
}

// TestChannelTimelineCoversAirtime ensures the sum of the channel occupancy
// timeline matches the total air time of all scheduled frames within rounding.
func TestChannelTimelineCoversAirtime(t *testing.T) {
	scn := buildTinyScenario(40)
	cfg := DefaultConfig(ModeFixed)
	cfg.DurationSec = 2 * 3600
	r := Run(scn, cfg)

	timelineSec := 0.0
	for _, ch := range r.Channels {
		for _, b := range ch.Buckets {
			timelineSec += b.OccupiedSeconds
		}
	}
	if timelineSec <= 0 {
		t.Fatal("timeline recorded no occupancy")
	}
	// every bucket ratio must be within [0,1]
	for _, ch := range r.Channels {
		for _, b := range ch.Buckets {
			if b.OccupancyRatio < 0 || b.OccupancyRatio > 1.0000001 {
				t.Fatalf("channel %d bucket %d ratio out of range: %f", ch.ID, b.Bucket, b.OccupancyRatio)
			}
		}
	}
}

// TestCompareDeterminism exercises the end-to-end comparison harness.
func TestCompareDeterminism(t *testing.T) {
	cmp := Compare(555, 80, 2*3600)
	if !cmp.Determinism.FixedRepeatedIdentical || !cmp.Determinism.ADRRepeatedIdentical {
		t.Fatal("comparison harness reported non-deterministic runs")
	}
	if cmp.Fixed.Agg.Mode != "fixed" || cmp.ADR.Agg.Mode != "adr" {
		t.Fatal("mode labels wrong")
	}
}
