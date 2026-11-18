package phy

import "math"

// Semtech demodulator SNR ceilings (LoRa modem, AN1200.13 / SX1276 datasheet):
// the minimum SNR at which a packet of each spreading factor can be decoded
// in the absence of interference.
var requiredSNR = map[SF]float64{
	SF7:  -7.5,
	SF8:  -10.0,
	SF9:  -12.5,
	SF10: -15.0,
	SF11: -17.5,
	SF12: -20.0,
}

// RequiredSNR returns the noise-limited demodulation SNR floor for sf.
func RequiredSNR(sf SF) float64 { return requiredSNR[sf] }

// NoiseFloor returns the receiver thermal-noise floor for bandwidth bwHz,
// anchored at -117 dBm for a 125 kHz LoRa channel (kT B + 6 dB noise figure).
func NoiseFloor(bwHz int) float64 {
	return -117.0 + 10.0*math.Log10(float64(bwHz)/125000.0)
}

// Sensitivity returns the receiver sensitivity for the given spreading factor
// and bandwidth: noise floor plus the demodulator SNR requirement.
func Sensitivity(sf SF, bwHz int) float64 {
	return NoiseFloor(bwHz) + RequiredSNR(sf)
}
