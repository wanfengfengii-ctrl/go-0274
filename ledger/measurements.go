package ledger

import (
	"math"

	"oyster-purification-release-gate/domain"
)

// WaterReading is one cage/timepoint water-quality observation. Every value is
// already a scaled integer produced by ParseScaled at the declared scale.
type WaterReading struct {
	CageSeal    string
	TimepointID string
	Turbidity   int64
	Salinity    int64
	Temperature int64
	Chlorine    int64
}

// ToxinReading is one plate-reader result for a cage/timepoint/hole. Raw values
// are scaled integers; the derived concentrations are computed in integer
// arithmetic only.
type ToxinReading struct {
	CageSeal    string
	TimepointID string
	Hole        string
	PSPRaw      int64
	DSPRaw      int64
	PSP         int64
	DSP         int64
}

// DeriveScaled computes (raw * factor) / divisor in integer arithmetic, the
// plate-reader calibration formula. It enforces the documented divide-by-zero
// and multiply-overflow checks and never uses floating point. The result is
// truncated toward zero, matching the fixed-scale representation.
func DeriveScaled(raw, factor, divisor int64) (int64, error) {
	if divisor == 0 {
		return 0, &domain.APIError{
			Code:    domain.CodeFixedPointOverflow,
			Message: "calibration divisor is zero",
		}
	}
	if raw > 0 && factor > 0 && raw > math.MaxInt64/factor {
		return 0, &domain.APIError{
			Code:    domain.CodeFixedPointOverflow,
			Message: "calibration multiplication overflows",
		}
	}
	if raw < 0 && factor > 0 && raw < math.MinInt64/factor {
		return 0, &domain.APIError{
			Code:    domain.CodeFixedPointOverflow,
			Message: "calibration multiplication overflows",
		}
	}
	return raw * factor / divisor, nil
}

// DeriveToxin fills the derived PSP/DSP fields of a reading using the given
// calibration factor and divisor. A failed derivation leaves the reading's
// derived values untouched so the caller can surface the stable error.
func (r *ToxinReading) DeriveToxin(factor, divisor int64) error {
	psp, err := DeriveScaled(r.PSPRaw, factor, divisor)
	if err != nil {
		return err
	}
	dsp, err := DeriveScaled(r.DSPRaw, factor, divisor)
	if err != nil {
		return err
	}
	r.PSP = psp
	r.DSP = dsp
	return nil
}
