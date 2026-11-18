// Package scenario deterministically generates the apiary node deployment:
// node positions, reporting periods, payload sizes and the fixed spreading
// factor that an installer would have assigned manually.
package scenario

import (
	"math"
	"math/rand/v2"

	"lorasim/internal/phy"
)

// NodeSpec is the immutable description of one sensor node; the same spec is
// fed to both the fixed-SF and the ADR run so comparisons are apples-to-apples.
type NodeSpec struct {
	ID              int
	X, Y            float64 // meters, gateway at origin
	Distance        float64 // meters from gateway
	ReportPeriodSec int
	PayloadBytes    int
	StartSF         phy.SF
	StartPowerDBm   int
	ShadowDB        float64 // static log-normal shadowing draw for this node
	Seed            int64   // base seed of the node's private RNG stream
}

// Scenario is one generated deployment.
type Scenario struct {
	Seed          int64
	NodeCount     int
	RadiusM       float64
	GatewayX      float64
	GatewayY      float64
	Nodes         []NodeSpec
	PathLossRefDB float64 // path loss at 1 m
	PathLossExp   float64 // log-distance exponent
}

// Config controls scenario generation.
type Config struct {
	Seed        int64
	NodeCount   int
	RadiusM     float64
	StartPower  int
	PayloadBase int
}

// DefaultConfig returns the baseline deployment parameters: a few hundred hive
// scales in a 3 km radius, mix of reporting cadences, ~18 byte frames.
func DefaultConfig(seed int64) Config {
	return Config{Seed: seed, NodeCount: 300, RadiusM: 3000, StartPower: 14, PayloadBase: 18}
}

// Generate builds a deterministic deployment. Every random draw comes from one
// master rand.Rng seeded with cfg.Seed, so identical seeds reproduce the exact
// same scenario (positions, cadences, shadowing, per-node RNG sub-streams).
func Generate(cfg Config) *Scenario {
	rng := rand.New(rand.NewPCG(uint64(cfg.Seed), uint64(cfg.Seed)^0x9e3779b97f4a7c15))

	s := &Scenario{
		Seed:          cfg.Seed,
		NodeCount:     cfg.NodeCount,
		RadiusM:       cfg.RadiusM,
		PathLossRefDB: 35.0, // PL @ 1 m, calibrated sub-GHz LoRa link
		PathLossExp:   2.8,  // mixed open/vegetated farm terrain
	}

	for id := 0; id < cfg.NodeCount; id++ {
		// Uniform point in a disk: sqrt(u) avoids center clustering.
		ang := rng.Float64() * 2 * math.Pi
		r := cfg.RadiusM * math.Sqrt(rng.Float64())
		x, y := r*math.Cos(ang), r*math.Sin(ang)
		dist := math.Hypot(x, y)

		// Reporting cadence mix: 60 % every 15 min, 30 % every 30 min,
		// 10 % high-rate every 5 min (hives being monitored closely).
		roll := rng.Float64()
		var period int
		switch {
		case roll < 0.60:
			period = 15 * 60
		case roll < 0.90:
			period = 30 * 60
		default:
			period = 5 * 60
		}

		payload := cfg.PayloadBase + rng.IntN(6) // 18..23 bytes

		// Manual installer assignment: distance buckets, deliberately
		// conservative (the SF the installer "sets and forgets").
		var sf phy.SF
		switch {
		case dist <= 700:
			sf = phy.SF7
		case dist <= 1300:
			sf = phy.SF8
		case dist <= 1900:
			sf = phy.SF9
		case dist <= 2400:
			sf = phy.SF10
		case dist <= 2800:
			sf = phy.SF11
		default:
			sf = phy.SF12
		}

		shadow := rng.NormFloat64() * 4.0 // static 4 dB log-normal shadowing

		s.Nodes = append(s.Nodes, NodeSpec{
			ID:              id,
			X:               x,
			Y:               y,
			Distance:        dist,
			ReportPeriodSec: period,
			PayloadBytes:    payload,
			StartSF:         sf,
			StartPowerDBm:   cfg.StartPower,
			ShadowDB:        shadow,
			Seed:            rng.Int64(),
		})
	}
	return s
}

// PathLoss returns the deterministic mean path loss between a node and the
// gateway (log-distance model). Fast fading is added per packet by the engine.
func (s *Scenario) PathLoss(n *NodeSpec) float64 {
	d := math.Max(n.Distance, 1.0)
	return s.PathLossRefDB + 10.0*s.PathLossExp*math.Log10(d) + n.ShadowDB
}
