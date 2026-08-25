package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"oyster-purification-release-gate/domain"
)

// Scales records the fixed-decimal precision, expressed as a decimal scale, for
// every measurement captured during a release. All values are stored as scaled
// integers; no floating point is ever used for thresholds or comparisons.
type Scales struct {
	SalinityDecimals    int
	TemperatureDecimals int
	ChlorineDecimals    int
	TurbidityDecimals   int
	PSPDecimals         int
	DSPDecimals         int
	NorovirusDecimals   int
	ColiformDecimals    int
}

// DefaultScales returns the field precisions defined by the project document.
// Salinity, temperature and residual chlorine use two decimals, turbidity one,
// the two algal toxins three, norovirus Ct one and coliform zero (an integer).
func DefaultScales() Scales {
	return Scales{
		SalinityDecimals:    2,
		TemperatureDecimals: 2,
		ChlorineDecimals:    2,
		TurbidityDecimals:   1,
		PSPDecimals:         3,
		DSPDecimals:         3,
		NorovirusDecimals:   1,
		ColiformDecimals:    0,
	}
}

// Thresholds holds every integer threshold a snapshot enforces. Mortality is
// expressed in permille of the locked sample count to stay integer-only.
type Thresholds struct {
	MaxMortalityPermille int64
	MaxPSP               int64
	MaxDSP               int64
	MaxNorovirusCt       int64
	MaxColiform          int64
	MinSalinity          int64
	MaxSalinity          int64
	MaxTemperature       int64
	MaxChlorine          int64
	MaxTurbidity         int64
}

// Digest computes the canonical normalized summary of a snapshot's measurable
// content. Any change to scales, thresholds, tides or the area produces a new
// digest, which is what lets stale submissions be detected deterministically.
func Digest(areaID string, tides []TidePattern, scales Scales, thresholds Thresholds) string {
	var b strings.Builder
	fmt.Fprintf(&b, "area=%s\n", areaID)
	sortedTides := append([]TidePattern(nil), tides...)
	sort.Slice(sortedTides, func(i, j int) bool { return sortedTides[i] < sortedTides[j] })
	for _, t := range sortedTides {
		fmt.Fprintf(&b, "tide=%s\n", t)
	}
	fmt.Fprintf(&b, "sal=%d temp=%d chl=%d turb=%d psp=%d dsp=%d noro=%d coli=%d\n",
		scales.SalinityDecimals, scales.TemperatureDecimals, scales.ChlorineDecimals,
		scales.TurbidityDecimals, scales.PSPDecimals, scales.DSPDecimals,
		scales.NorovirusDecimals, scales.ColiformDecimals)
	fmt.Fprintf(&b, "mort=%d psp=%d dsp=%d noro=%d coli=%d sal=%d..%d temp=%d chl=%d turb=%d\n",
		thresholds.MaxMortalityPermille, thresholds.MaxPSP, thresholds.MaxDSP,
		thresholds.MaxNorovirusCt, thresholds.MaxColiform, thresholds.MinSalinity,
		thresholds.MaxSalinity, thresholds.MaxTemperature, thresholds.MaxChlorine,
		thresholds.MaxTurbidity)
	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ErrUnknownSnapshot is returned when a rule-snapshot identifier is not in the
// catalog. It maps to STALE_RULE_DIGEST at the API boundary because an unknown
// snapshot can never be trusted for a lock.
var ErrUnknownSnapshot = &domain.APIError{
	Code:    domain.CodeStaleRuleDigest,
	Message: "unknown rule snapshot",
}
