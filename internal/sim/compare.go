// Package sim also exposes the end-to-end comparison harness used by both the
// CLI and the web server: one generated scenario, two runs (fixed SF and ADR),
// plus a byte-level determinism check.
package sim

import (
	"bytes"
	"encoding/json"

	"lorasim/internal/scenario"
)

// Comparison is the output of the fixed-SF vs ADR comparison.
type Comparison struct {
	ScenarioSeed     int64             `json:"scenarioSeed"`
	NodeCount        int               `json:"nodeCount"`
	Fixed            *Report           `json:"fixed"`
	ADR              *Report           `json:"adr"`
	Determinism      DeterminismResult `json:"determinism"`
	DeliveryDeltaPct float64           `json:"deliveryDeltaPct"`
	BatteryDeltaDays float64           `json:"batteryDeltaDays"`
}

// DeterminismResult records whether repeating the same run reproduces the
// exact same JSON byte stream.
type DeterminismResult struct {
	FixedRepeatedIdentical bool `json:"fixedRepeatedIdentical"`
	ADRRepeatedIdentical   bool `json:"adrRepeatedIdentical"`
}

// Compare generates one scenario from seed and runs it three times: fixed-SF
// twice and ADR twice, so the determinism requirement ("same seed, two runs,
// byte-identical output") is verified inside the harness itself.
func Compare(seed int64, nodeCount int, durationSec int64) *Comparison {
	scfg := scenario.DefaultConfig(seed)
	scfg.NodeCount = nodeCount
	scn := scenario.Generate(scfg)

	fixedCfg := DefaultConfig(ModeFixed)
	fixedCfg.DurationSec = durationSec
	adrCfg := DefaultConfig(ModeADR)
	adrCfg.DurationSec = durationSec

	fixed1 := Run(scn, fixedCfg)
	fixed2 := Run(scn, fixedCfg)
	adr1 := Run(scn, adrCfg)
	adr2 := Run(scn, adrCfg)

	cmp := &Comparison{
		ScenarioSeed: seed,
		NodeCount:    nodeCount,
		Fixed:        fixed1,
		ADR:          adr1,
		Determinism: DeterminismResult{
			FixedRepeatedIdentical: jsonEqual(fixed1, fixed2),
			ADRRepeatedIdentical:   jsonEqual(adr1, adr2),
		},
		DeliveryDeltaPct: (adr1.Agg.NetworkDeliveryRate - fixed1.Agg.NetworkDeliveryRate) * 100,
		BatteryDeltaDays: adr1.Agg.MedianBatteryDays - fixed1.Agg.MedianBatteryDays,
	}
	return cmp
}

func jsonEqual(a, b any) bool {
	ba, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(ba, bb)
}
