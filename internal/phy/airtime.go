// Package phy implements the LoRa physical-layer primitives used by the
// simulator: exact airtime calculation, EU868 sub-band/duty-cycle layout and
// receiver sensitivity tables.
package phy

import "math"

// SF is a LoRa spreading factor (7..12).
type SF uint8

const (
	SF7  SF = 7
	SF8  SF = 8
	SF9  SF = 9
	SF10 SF = 10
	SF11 SF = 11
	SF12 SF = 12
)

// Valid reports whether sf is a legal LoRa spreading factor.
func (sf SF) Valid() bool { return sf >= SF7 && sf <= SF12 }

// RadioCfg describes one LoRa radio configuration.
type RadioCfg struct {
	SF              SF
	BWHz            int  // bandwidth in Hz, e.g. 125000
	CRCode          int  // coding-rate code 1..4 meaning 4/5..4/8
	PreambleSymbols int  // explicit preamble length in symbols
	LDRO            bool // low-data-rate optimization enabled
	ExplicitHeader  bool // false = implicit header
	CRC             bool // payload CRC present
	TxPowerDBm      int  // transmit power in dBm
}

// SymbolTime returns the LoRa symbol duration T_sym = 2^SF / BW in seconds.
func SymbolTime(c RadioCfg) float64 {
	return float64(int64(1)<<uint(c.SF)) / float64(c.BWHz)
}

// LDRONeeded reports whether the low-data-rate optimization should be turned
// on for the given configuration. Semtech recommendation: LDRO when
// T_sym > 16 ms (2^SF / BW > 16 ms).
func LDRONeeded(sf SF, bwHz int) bool {
	return float64(bwHz) < float64(uint64(1)<<uint(sf))*1000.0/16.0
}

// Airtime breaks down the time-on-air of one LoRa frame.
type Airtime struct {
	SymbolTimeSec   float64
	PreambleSymbols float64
	PayloadSymbols  int
	PreambleSec     float64
	PayloadSec      float64
	TotalSec        float64
	TotalNS         int64 // TotalSec rounded to whole nanoseconds
}

// AirtimeCalc computes LoRa time-on-air with the Semtech/SX1276 formula,
// which is the same formula implemented by the Semtech LoRa Calculator
// (SX1261/SX1262/SX1276 "LoRa Calculator" spreadsheet, AN1200.13):
//
//	T_sym   = 2^SF / BW
//	T_pre   = (n_pre + 4.25) * T_sym
//	n_pay   = 8 + max( ceil( (8*PL - 4*SF + 28 + 16*CRC - 20*IH) /
//	                         (4 * (SF - 2*DE)) ) * CR, 0 )
//	T_pay   = n_pay * T_sym
//	ToA     = T_pre + T_pay
//
// DE is the low-data-rate-optimization flag and IH is the implicit-header
// flag. All inputs come from RadioCfg; payloadBytes is the MAC payload size.
func AirtimeCalc(payloadBytes int, c RadioCfg) Airtime {
	if !c.SF.Valid() || c.BWHz <= 0 || c.CRCode < 1 || c.CRCode > 4 {
		panic("lorasim/phy: invalid radio configuration")
	}
	tsym := SymbolTime(c)

	ih := 0
	if !c.ExplicitHeader {
		ih = 1
	}
	crc := 0
	if c.CRC {
		crc = 1
	}
	de := 0
	if c.LDRO {
		de = 1
	}

	num := 8.0*float64(payloadBytes) - 4.0*float64(c.SF) + 28.0 +
		16.0*float64(crc) - 20.0*float64(ih)
	den := 4.0 * float64(int(c.SF)-2*de)
	payloadSym := 8 + math.Max(math.Ceil(num/den)*float64(c.CRCode), 0)

	preSym := float64(c.PreambleSymbols) + 4.25
	tPre := preSym * tsym
	tPay := payloadSym * tsym
	total := tPre + tPay

	return Airtime{
		SymbolTimeSec:   tsym,
		PreambleSymbols: preSym,
		PayloadSymbols:  int(payloadSym),
		PreambleSec:     tPre,
		PayloadSec:      tPay,
		TotalSec:        total,
		TotalNS:         int64(math.Round(total * 1e9)),
	}
}
