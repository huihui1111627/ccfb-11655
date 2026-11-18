package scenario

import (
	"testing"
)

// TestGenerateDeterministic requires identical seeds to reproduce the exact
// deployment (positions, cadences, payload sizes, shadowing, sub-streams).
func TestGenerateDeterministic(t *testing.T) {
	a := Generate(DefaultConfig(20261119))
	b := Generate(DefaultConfig(20261119))
	if a.NodeCount != b.NodeCount || len(a.Nodes) != 300 {
		t.Fatal("node count mismatch")
	}
	for i := range a.Nodes {
		if a.Nodes[i] != b.Nodes[i] {
			t.Fatalf("node %d differs between equal-seed scenarios", i)
		}
	}
}

// TestGenerateDifferentSeeds ensures distinct seeds produce distinct
// deployments rather than an accidentally constant generator.
func TestGenerateDifferentSeeds(t *testing.T) {
	a := Generate(DefaultConfig(11))
	b := Generate(DefaultConfig(22))
	same := 0
	for i := range a.Nodes {
		if a.Nodes[i] == b.Nodes[i] {
			same++
		}
	}
	if same == len(a.Nodes) {
		t.Fatal("different seeds produced identical deployments")
	}
}

// TestPositionsInsideRadiusAndPeriodsKnown bounds the generated geometry and
// confirms the reporting-cadence mix only uses the three configured periods.
func TestPositionsInsideRadiusAndPeriodsKnown(t *testing.T) {
	s := Generate(DefaultConfig(7))
	periods := map[int]bool{}
	for _, n := range s.Nodes {
		if n.Distance > s.RadiusM+1e-9 {
			t.Fatalf("node %d outside deployment radius", n.ID)
		}
		if n.Distance < 0 {
			t.Fatalf("node %d negative distance", n.ID)
		}
		periods[n.ReportPeriodSec] = true
		if n.PayloadBytes < 18 || n.PayloadBytes > 23 {
			t.Fatalf("node %d payload out of generated range: %d", n.ID, n.PayloadBytes)
		}
		if !n.StartSF.Valid() {
			t.Fatalf("node %d invalid start SF", n.ID)
		}
	}
	for _, want := range []int{300, 900, 1800} {
		if !periods[want] {
			t.Fatalf("expected reporting period %ds in the cadence mix", want)
		}
	}
}
