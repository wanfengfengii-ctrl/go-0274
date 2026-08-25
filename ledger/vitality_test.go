package ledger

import (
	"errors"
	"testing"

	"oyster-purification-release-gate/domain"
)

func TestVitalityConservationAcceptsExactSum(t *testing.T) {
	v := VitalityCounts{Closed: 8, WeakOpen: 1, Dead: 1, Broken: 0}
	if err := v.ValidateConservation(10); err != nil {
		t.Fatalf("expected conserved counts accepted, got %v", err)
	}
}

func TestVitalityConservationRejectsMismatch(t *testing.T) {
	v := VitalityCounts{Closed: 8, WeakOpen: 1, Dead: 1, Broken: 1}
	err := v.ValidateConservation(10)
	var apiErr *domain.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != domain.CodeCoverageInvalid {
		t.Fatalf("expected COVERAGE_INVALID, got %v", err)
	}
}

func TestVitalityConservationRejectsNegative(t *testing.T) {
	v := VitalityCounts{Closed: -1, WeakOpen: 0, Dead: 0, Broken: 0}
	if err := v.ValidateConservation(0); err == nil {
		t.Fatalf("negative count should be rejected")
	}
}
