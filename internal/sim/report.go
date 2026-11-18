package sim

import (
	"math"
	"sort"

	"lorasim/internal/phy"
)

// NodeReport is the per-node view rendered on the map and battery panels.
type NodeReport struct {
	ID              int            `json:"id"`
	X               float64        `json:"x"`
	Y               float64        `json:"y"`
	Distance        float64        `json:"distance"`
	CurrentSF       int            `json:"currentSF"`
	CurrentPowerDBm int            `json:"currentPower"`
	ReportPeriodSec int            `json:"reportPeriodSec"`
	ULAttempted     int            `json:"ulAttempted"`
	ULSent          int            `json:"ulSent"`
	ULReceived      int            `json:"ulReceived"`
	ULAcked         int            `json:"ulAcked"`
	DCBlocked       int            `json:"dcBlocked"`
	DeliveryRate    float64        `json:"deliveryRate"`
	BatteryUsedMAh  float64        `json:"batteryUsedMah"`
	BatteryLifeDays float64        `json:"batteryLifeDays"`
	SFHistogram     map[int]int    `json:"sfHistogram"`
	LossReasons     map[string]int `json:"lossReasons"`
}

// ChannelBucket is one timeline bucket for one channel.
type ChannelBucket struct {
	Bucket          int     `json:"bucket"`
	OccupiedSeconds float64 `json:"occupiedSeconds"`
	OccupancyRatio  float64 `json:"occupancyRatio"`
}

// ChannelReport is the occupancy timeline of one frequency channel.
type ChannelReport struct {
	ID        int             `json:"id"`
	FreqHz    int64           `json:"freqHz"`
	SubBandID int             `json:"subBandId"`
	DutyCycle float64         `json:"dutyCycle"`
	Buckets   []ChannelBucket `json:"buckets"`
	PeakRatio float64         `json:"peakRatio"`
	MeanRatio float64         `json:"meanRatio"`
}

// Aggregate is the network-wide summary used for the fixed-vs-ADR comparison.
type Aggregate struct {
	Mode                string         `json:"mode"`
	DurationSec         int64          `json:"durationSec"`
	NodeCount           int            `json:"nodeCount"`
	ULAttempted         int            `json:"ulAttempted"`
	ULSent              int            `json:"ulSent"`
	ULReceived          int            `json:"ulReceived"`
	ULAcked             int            `json:"ulAcked"`
	NetworkDeliveryRate float64        `json:"networkDeliveryRate"`
	MedianNodePDR       float64        `json:"medianNodePdr"`
	DCBlocked           int            `json:"dcBlocked"`
	SFDistribution      map[string]int `json:"sfDistribution"`
	LossReasons         map[string]int `json:"lossReasons"`
	DLLossReasons       map[string]int `json:"dlLossReasons"`
	DLCount             int            `json:"dlCount"`
	MedianBatteryDays   float64        `json:"medianBatteryDays"`
	MeanBatteryDays     float64        `json:"meanBatteryDays"`
	BatteryTotalMAh     float64        `json:"batteryTotalMah"`
	ChannelPeakMean     float64        `json:"channelPeakMean"`
	ChannelOverallMean  float64        `json:"channelOverallMean"`
}

// Report is the complete deterministic output of one run.
type Report struct {
	Seed       int64           `json:"seed"`
	Agg        Aggregate       `json:"aggregate"`
	Nodes      []NodeReport    `json:"nodes"`
	Channels   []ChannelReport `json:"channels"`
	ConfigHash string          `json:"configHash"`
}

func (e *Engine) buildReport() *Report {
	r := &Report{Seed: e.scn.Seed, ConfigHash: configFingerprint(e.cfg)}

	// deterministic node ordering by ID
	nodes := make([]NodeReport, 0, len(e.nodes))
	pdrs := make([]float64, 0, len(e.nodes))
	lifeDays := make([]float64, 0, len(e.nodes))
	var totalMAh float64
	totalReceived := 0
	for _, n := range e.nodes {
		pdr := 0.0
		if n.ulSent > 0 {
			pdr = float64(n.ulReceived) / float64(n.ulSent)
		}
		pdrs = append(pdrs, pdr)
		life := e.batteryLifeDays(n)
		if !math.IsNaN(life) {
			lifeDays = append(lifeDays, life)
		}
		totalMAh += n.mAhUsed
		totalReceived += n.ulReceived

		nr := NodeReport{
			ID:              n.spec.ID,
			X:               n.spec.X,
			Y:               n.spec.Y,
			Distance:        n.spec.Distance,
			CurrentSF:       int(n.sf),
			CurrentPowerDBm: n.powerDBm,
			ReportPeriodSec: n.spec.ReportPeriodSec,
			ULAttempted:     n.ulAttempted,
			ULSent:          n.ulSent,
			ULReceived:      n.ulReceived,
			ULAcked:         n.ulAcked,
			DCBlocked:       n.dcBlocked,
			DeliveryRate:    pdr,
			BatteryUsedMAh:  n.mAhUsed,
			BatteryLifeDays: life,
			SFHistogram:     map[int]int{},
			LossReasons:     map[string]int{},
		}
		for sf, c := range n.sfFrames {
			nr.SFHistogram[int(sf)] = c
		}
		for k, c := range n.lossReasons {
			nr.LossReasons[k] = c
		}
		nodes = append(nodes, nr)
	}
	r.Nodes = nodes

	// channels and occupancy timeline. The RX2 downlink-only channel is still
	// reported with its timeline, but it is excluded from the uplink-channel
	// peak/mean aggregates.
	bucketLenSec := float64(e.bucketSec) / 1e9
	peakSum, meanSum := 0.0, 0.0
	chCount := e.band.UplinkChannelCount()
	for _, ch := range e.band.Channels {
		isUplink := ch.ID < e.band.UplinkChannelCount()
		cr := ChannelReport{
			ID:        ch.ID,
			FreqHz:    ch.FreqHz,
			SubBandID: ch.SubBandID,
			DutyCycle: e.band.SubBandByID(ch.SubBandID).DutyCycle,
			Buckets:   []ChannelBucket{}, // never emit a JSON null
		}
		buckets := e.timeline[ch.ID]
		ids := make([]int, 0, len(buckets))
		for b := range buckets {
			ids = append(ids, b)
		}
		sort.Ints(ids)
		peak := 0.0
		sum := 0.0
		for _, b := range ids {
			occ := buckets[b]
			ratio := occ / bucketLenSec
			sum += ratio
			if ratio > peak {
				peak = ratio
			}
			cr.Buckets = append(cr.Buckets, ChannelBucket{
				Bucket: b, OccupiedSeconds: occ, OccupancyRatio: ratio,
			})
		}
		cr.PeakRatio = peak
		nBuckets := math.Max(1, float64(len(ids)))
		cr.MeanRatio = sum / nBuckets
		if isUplink {
			peakSum += peak
			meanSum += sum / nBuckets
		}
		r.Channels = append(r.Channels, cr)
	}

	netPDR := 0.0
	if e.ulSent > 0 {
		netPDR = float64(totalReceived) / float64(e.ulSent)
	}
	sfDist := map[string]int{}
	for sf := phy.SF7; sf <= phy.SF12; sf++ {
		sfDist["SF"+itoa(int(sf))] = e.sfDist[sf]
	}
	dlCount := 0
	for _, c := range e.dlLoss {
		dlCount += c
	}
	dlCount += e.ulAcked

	r.Agg = Aggregate{
		Mode:                e.cfg.Mode.String(),
		DurationSec:         e.cfg.DurationSec,
		NodeCount:           len(e.nodes),
		ULAttempted:         e.ulAttempted,
		ULSent:              e.ulSent,
		ULReceived:          totalReceived,
		ULAcked:             e.ulAcked,
		NetworkDeliveryRate: netPDR,
		MedianNodePDR:       median(pdrs),
		DCBlocked:           e.ulDroppedDC,
		SFDistribution:      sfDist,
		LossReasons:         copyStringMap(e.lossReasons),
		DLLossReasons:       copyStringMap(e.dlLoss),
		DLCount:             dlCount,
		MedianBatteryDays:   median(lifeDays),
		MeanBatteryDays:     meanf(lifeDays),
		BatteryTotalMAh:     totalMAh,
		ChannelPeakMean:     peakSum / float64(chCount),
		ChannelOverallMean:  meanSum / float64(chCount),
	}
	return r
}

func copyStringMap(m map[string]int) map[string]int {
	out := map[string]int{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	cp := append([]float64(nil), xs...)
	sort.Float64s(cp)
	n := len(cp)
	if n%2 == 1 {
		return cp[n/2]
	}
	return (cp[n/2-1] + cp[n/2]) / 2
}

func meanf(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
