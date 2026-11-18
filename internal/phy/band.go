package phy

// Channel is one fixed LoRa uplink frequency.
type Channel struct {
	ID        int
	FreqHz    int64
	SubBandID int
}

// SubBand groups channels sharing one regulatory duty-cycle limit.
type SubBand struct {
	ID        int
	Label     string
	FreqMinHz int64
	FreqMaxHz int64
	DutyCycle float64 // fraction of air time allowed, e.g. 0.01 = 1%
}

// Band describes a regional channel plan.
type Band struct {
	Name      string
	Channels  []Channel
	SubBands  []SubBand
	RX2FreqHz int64
	RX2SF     SF
}

// ChannelByID returns the channel with the given id.
func (b *Band) ChannelByID(id int) Channel {
	return b.Channels[id]
}

// SubBandByID returns the sub-band with the given id.
func (b *Band) SubBandByID(id int) SubBand {
	for _, sb := range b.SubBands {
		if sb.ID == id {
			return sb
		}
	}
	panic("lorasim/phy: unknown sub-band")
}

// SubBandOfChannel returns the sub-band of a channel.
func (b *Band) SubBandOfChannel(chID int) SubBand {
	return b.SubBandByID(b.Channels[chID].SubBandID)
}

// EU868 returns the EU868-style channel plan used by the simulator.
//
// The gateway listens on eight uplink channels spread over three regulatory
// sub-bands (ETSI EN 300 220 / LoRaWAN EU868 regional parameters):
//
//	g1 868.0-868.6 MHz: 868.1/868.3/868.5, 1 % duty cycle
//	g2 868.7-869.2 MHz: 868.8,                 0.1 % duty cycle
//	g4 867.0-868.0 MHz: 867.1/.3/.5/.7/.9,     0.1 % duty cycle
//
// RX2 uses the 869.525 MHz g3 channel with a 10 % duty-cycle allowance.
func EU868() *Band {
	return &Band{
		Name: "EU868",
		SubBands: []SubBand{
			{ID: 0, Label: "g1 868.0-868.6", FreqMinHz: 868000000, FreqMaxHz: 868600000, DutyCycle: 0.01},
			{ID: 1, Label: "g2 868.7-869.2", FreqMinHz: 868700000, FreqMaxHz: 869200000, DutyCycle: 0.001},
			{ID: 2, Label: "g4 867.0-868.0", FreqMinHz: 867000000, FreqMaxHz: 868000000, DutyCycle: 0.001},
			{ID: 3, Label: "g3 869.4-869.65", FreqMinHz: 869400000, FreqMaxHz: 869650000, DutyCycle: 0.10},
		},
		Channels: []Channel{
			{ID: 0, FreqHz: 868100000, SubBandID: 0},
			{ID: 1, FreqHz: 868300000, SubBandID: 0},
			{ID: 2, FreqHz: 868500000, SubBandID: 0},
			{ID: 3, FreqHz: 868800000, SubBandID: 1},
			{ID: 4, FreqHz: 867100000, SubBandID: 2},
			{ID: 5, FreqHz: 867300000, SubBandID: 2},
			{ID: 6, FreqHz: 867500000, SubBandID: 2},
			{ID: 7, FreqHz: 867700000, SubBandID: 2},
			// ID 8 is the RX2 downlink-only channel on the g3 10 % sub-band.
			{ID: 8, FreqHz: 869525000, SubBandID: 3},
		},
		RX2FreqHz: 869525000,
		RX2SF:     SF12,
	}
}

// UplinkChannelCount is the number of channels nodes may transmit on. The
// channel at index RX2ChannelID is reserved for Class A RX2 downlinks.
func (b *Band) UplinkChannelCount() int { return len(b.Channels) - 1 }

// RX2Channel returns the reserved channel description for the RX2 window.
func (b *Band) RX2Channel() Channel {
	return b.Channels[len(b.Channels)-1]
}
