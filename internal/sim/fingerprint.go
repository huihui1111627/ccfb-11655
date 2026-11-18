package sim

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"

	"lorasim/internal/phy"
)

// configFingerprint renders every value that can change a run into a stable
// textual form, then hashes it. Two reports produced by the same seed and
// identical configuration carry the same fingerprint.
func configFingerprint(cfg Config) string {
	var b strings.Builder
	b.WriteString("v1|")
	b.WriteString(cfg.Mode.String())
	b.WriteString("|bw=")
	b.WriteString(strconv.Itoa(cfg.BWHz))
	b.WriteString("|rx2bw=")
	b.WriteString(strconv.Itoa(cfg.RX2BWHz))
	b.WriteString("|cr=")
	b.WriteString(strconv.Itoa(cfg.CRCode))
	b.WriteString("|pre=")
	b.WriteString(strconv.Itoa(cfg.Preamble))
	b.WriteString("|ldro=")
	b.WriteString(strconv.FormatBool(cfg.AutoLDRO))
	b.WriteString("|cap=")
	b.WriteString(strconv.FormatFloat(cfg.CaptureMargin, 'f', 3, 64))
	b.WriteString("|xsf=")
	b.WriteString(strconv.FormatFloat(cfg.CrossSFSIRDB, 'f', 3, 64))
	b.WriteString("|rx1=")
	b.WriteString(strconv.FormatInt(cfg.RX1DelayMS, 10))
	b.WriteString("|rx2=")
	b.WriteString(strconv.FormatInt(cfg.RX2DelayMS, 10))
	b.WriteString("|capsf=")
	var sfs []int
	for sf := range cfg.DemodPerSF {
		sfs = append(sfs, int(sf))
	}
	sort.Ints(sfs)
	for _, sf := range sfs {
		b.WriteString(strconv.Itoa(sf))
		b.WriteString(":")
		b.WriteString(strconv.Itoa(cfg.DemodPerSF[phy.SF(sf)]))
		b.WriteString(",")
	}
	b.WriteString("|captot=")
	b.WriteString(strconv.Itoa(cfg.DemodTotalCap))
	b.WriteString("|gwtx=")
	b.WriteString(strconv.Itoa(cfg.GatewayTxPower))
	b.WriteString("|hist=")
	b.WriteString(strconv.Itoa(cfg.ADRHistoryFrames))
	b.WriteString("|margin=")
	b.WriteString(strconv.FormatFloat(cfg.ADRTargetMargin, 'f', 3, 64))
	b.WriteString("|cmd=")
	b.WriteString(strconv.Itoa(cfg.ADRCmdEvery))
	b.WriteString("|fail=")
	b.WriteString(strconv.Itoa(cfg.NodeFailLimit))
	b.WriteString("|step=")
	b.WriteString(strconv.Itoa(cfg.NodeFailStep))
	b.WriteString("|dur=")
	b.WriteString(strconv.FormatInt(cfg.DurationSec, 10))
	h := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(h[:16])
}
