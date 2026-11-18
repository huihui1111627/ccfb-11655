// Command lorasim runs the fixed-SF vs ADR comparison from the terminal and
// writes machine-readable JSON plus a human-readable summary.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"lorasim/internal/sim"
)

func main() {
	seed := flag.Int64("seed", 20261119, "scenario RNG seed (same seed reproduces every draw)")
	nodes := flag.Int("nodes", 300, "number of sensor nodes")
	hours := flag.Int("hours", 6, "simulated network time in hours")
	out := flag.String("out", "", "write the full comparison JSON to this path")
	flag.Parse()

	start := time.Now()
	cmp := sim.Compare(*seed, *nodes, int64(*hours)*3600)
	elapsed := time.Since(start)

	if *out != "" {
		data, err := json.MarshalIndent(cmp, "", "  ")
		if err != nil {
			fatal(err)
		}
		if err := os.WriteFile(*out, data, 0o644); err != nil {
			fatal(err)
		}
	}

	printSummary(cmp, elapsed)
}

func printSummary(cmp *sim.Comparison, elapsed time.Duration) {
	f, a := cmp.Fixed.Agg, cmp.ADR.Agg
	fmt.Printf("LoRa apiary simulation  seed=%d  nodes=%d  duration=%dh  wall=%s\n",
		cmp.ScenarioSeed, cmp.NodeCount, f.DurationSec/3600, elapsed.Round(time.Millisecond))
	fmt.Println("------------------------------------------------------------")
	fmt.Printf("%-28s %12s %12s %10s\n", "metric", "fixed SF", "ADR", "delta")
	row := func(label string, fx, ad, delta string) {
		fmt.Printf("%-28s %12s %12s %10s\n", label, fx, ad, delta)
	}
	row("uplink frames attempted", itoa(f.ULAttempted), itoa(a.ULAttempted),
		itoa(a.ULAttempted-f.ULAttempted))
	row("network delivery rate", pct(f.NetworkDeliveryRate), pct(a.NetworkDeliveryRate),
		fmt.Sprintf("%+.2f pt", cmp.DeliveryDeltaPct))
	row("median node PDR", pct(f.MedianNodePDR), pct(a.MedianNodePDR),
		fmt.Sprintf("%+.2f pt", (a.MedianNodePDR-f.MedianNodePDR)*100))
	row("downlinks delivered", itoa(f.ULAcked), itoa(a.ULAcked),
		itoa(a.ULAcked-f.ULAcked))
	row("duty-cycle blocked frames", itoa(f.DCBlocked), itoa(a.DCBlocked),
		itoa(a.DCBlocked-f.DCBlocked))
	row("median battery life (days)", days(f.MedianBatteryDays), days(a.MedianBatteryDays),
		fmt.Sprintf("%+.1f", cmp.BatteryDeltaDays))
	row("mean channel occupancy", pct(f.ChannelOverallMean), pct(a.ChannelOverallMean),
		fmt.Sprintf("%+.2f pt", (a.ChannelOverallMean-f.ChannelOverallMean)*100))
	fmt.Println("------------------------------------------------------------")
	fmt.Println("SF distribution (frames):")
	for sf := 7; sf <= 12; sf++ {
		key := fmt.Sprintf("SF%d", sf)
		fmt.Printf("  %s  fixed=%-8d adr=%-8d\n", key, f.SFDistribution[key], a.SFDistribution[key])
	}
	fmt.Println("loss reasons:")
	for _, reason := range sortedLossKeys(f.LossReasons, a.LossReasons) {
		fmt.Printf("  %-24s fixed=%-8d adr=%-8d\n", reason, f.LossReasons[reason], a.LossReasons[reason])
	}
	fmt.Println("------------------------------------------------------------")
	fmt.Printf("determinism: fixed repeat identical=%v  adr repeat identical=%v\n",
		cmp.Determinism.FixedRepeatedIdentical, cmp.Determinism.ADRRepeatedIdentical)
}

func sortedLossKeys(maps ...map[string]int) []string {
	seen := map[string]bool{}
	var keys []string
	for _, m := range maps {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	// simple insertion sort keeps output deterministic
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}

func pct(x float64) string  { return fmt.Sprintf("%.2f%%", x*100) }
func days(x float64) string { return fmt.Sprintf("%.1f", x) }
func itoa(i int) string     { return fmt.Sprintf("%d", i) }

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "lorasim:", err)
	os.Exit(1)
}
