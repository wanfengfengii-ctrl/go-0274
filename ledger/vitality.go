package ledger

import "oyster-purification-release-gate/domain"

// VitalityCounts holds the four non-negative counts recorded for a single
// cage/timepoint/observation-point cell.
type VitalityCounts struct {
	Closed   int64 // 闭壳
	WeakOpen int64 // 开壳弱活
	Dead     int64 // 死亡
	Broken   int64 // 破壳
}

// Total returns the sum of all four counts.
func (v VitalityCounts) Total() int64 {
	return v.Closed + v.WeakOpen + v.Dead + v.Broken
}

// ValidateConservation enforces the counting-conservation invariant: every
// count must be non-negative and their sum must equal the locked sample count.
// It returns COVERAGE_INVALID on any violation.
func (v VitalityCounts) ValidateConservation(locked int64) error {
	for _, c := range []int64{v.Closed, v.WeakOpen, v.Dead, v.Broken} {
		if c < 0 {
			return &domain.APIError{
				Code:    domain.CodeCoverageInvalid,
				Message: "vitality counts must be non-negative",
			}
		}
	}
	if v.Total() != locked {
		return &domain.APIError{
			Code:    domain.CodeCoverageInvalid,
			Message: "vitality counts do not conserve the locked sample count",
		}
	}
	return nil
}
