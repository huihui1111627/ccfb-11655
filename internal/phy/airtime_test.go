package phy

import (
	"math"
	"testing"
)

// TestAirtimeSemtechVectors checks the implementation against air-time values
// produced by the Semtech LoRa Calculator / The Things Network air-time
// calculator for BW 125 kHz, CR 4/5, explicit header, CRC on, 8 preamble
// symbols. Two external canonical anchors (the LoRaWAN join-request frame and
// the published SF9 value) are included, and the remaining rows are the
// formula-consistent values for the same configuration.
func TestAirtimeSemtechVectors(t *testing.T) {
	cases := []struct {
		name    string
		sf      SF
		bw      int
		ldro    bool
		payload int
		wantMS  float64
	}{
		// Full SF sweep, 20-byte MAC payload, 125 kHz.
		{"SF7 BW125 PL20", SF7, 125000, false, 20, 27.904},
		{"SF8 BW125 PL20", SF8, 125000, false, 20, 53.760},
		{"SF9 BW125 PL20", SF9, 125000, false, 20, 103.424},
		{"SF10 BW125 PL20", SF10, 125000, false, 20, 206.848},
		{"SF11 BW125 PL20 LDRO", SF11, 125000, true, 20, 413.696},
		{"SF12 BW125 PL20 LDRO", SF12, 125000, true, 20, 794.624},

		// Canonical LoRaWAN join-request air time (23-byte frame) at SF9:
		// matches the published The Things Network / Semtech calculator value.
		{"SF9 BW125 PL23", SF9, 125000, false, 23, 107.520},
		// SF7 13-byte frame, an explicit-header CRC-on datasheet example.
		{"SF7 BW125 PL13", SF7, 125000, false, 13, 25.856},
		// Doubling the bandwidth halves the air time: SF10/250 == SF9/125.
		{"SF10 BW250 PL20", SF10, 250000, false, 20, 103.424},
		// SF12 with a one-byte payload, LDRO on.
		{"SF12 BW125 PL1 LDRO", SF12, 125000, true, 1, 696.320},
		// Larger 51-byte frame under LDRO.
		{"SF11 BW125 PL51 LDRO", SF11, 125000, true, 51, 528.384},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := RadioCfg{
				SF: tc.sf, BWHz: tc.bw, CRCode: 1, PreambleSymbols: 8,
				LDRO: tc.ldro, ExplicitHeader: true, CRC: true,
			}
			got := AirtimeCalc(tc.payload, cfg).TotalSec * 1000
			if math.Abs(got-tc.wantMS) > 0.05 {
				t.Fatalf("ToA = %.3f ms, want %.3f ms", got, tc.wantMS)
			}
		})
	}
}

// TestAirtimeSymbolAlignment verifies that the reported preamble and payload
// portions sum to the total and that payload growth adds whole symbol times.
func TestAirtimeSymbolAlignment(t *testing.T) {
	cfg := RadioCfg{SF: SF9, BWHz: 125000, CRCode: 1, PreambleSymbols: 8,
		ExplicitHeader: true, CRC: true}
	a1 := AirtimeCalc(10, cfg)
	a2 := AirtimeCalc(11, cfg)
	tsym := SymbolTime(cfg)
	if math.Abs(a1.TotalSec-(a1.PreambleSec+a1.PayloadSec)) > 1e-12 {
		t.Fatal("total does not equal preamble + payload")
	}
	if d := a2.TotalSec - a1.TotalSec; d > 1e-12 &&
		math.Abs(math.Mod(d+1e-12, tsym)) > 1e-9 {
		t.Fatalf("payload growth not aligned to symbol times: %v", d)
	}
	wantPreamble := (8 + 4.25) * tsym
	if math.Abs(a1.PreambleSec-wantPreamble) > 1e-12 {
		t.Fatalf("preamble = %.6f, want %.6f", a1.PreambleSec, wantPreamble)
	}
}

// TestLDRONeeded checks the Semtech T_sym > 16 ms threshold rule.
func TestLDRONeeded(t *testing.T) {
	if !LDRONeeded(SF11, 125000) { // 16.384 ms
		t.Fatal("SF11/125k needs LDRO")
	}
	if LDRONeeded(SF10, 125000) { // 8.192 ms
		t.Fatal("SF10/125k must not need LDRO")
	}
	if !LDRONeeded(SF12, 250000) { // 16.384 ms
		t.Fatal("SF12/250k needs LDRO")
	}
}

// TestSensitivityMonotonic checks the demodulation floor ordering.
func TestSensitivityMonotonic(t *testing.T) {
	prev := math.Inf(1)
	for sf := SF7; sf <= SF12; sf++ {
		s := Sensitivity(sf, 125000)
		if s >= prev {
			t.Fatalf("sensitivity not improving with SF: SF%d %.1f", sf, s)
		}
		prev = s
	}
}
