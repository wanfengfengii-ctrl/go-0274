package catalog

import (
	"errors"
	"testing"

	"oyster-purification-release-gate/domain"
)

var snap = RuleSnapshot{
	ID:      "rules-1",
	Version: 3,
	Digest:  "sha256:abc",
	AreaID:  "east",
	Tides:   []TidePattern{"T1", "T2"},
}

func TestValidateAreaTideAcceptsAllowedPair(t *testing.T) {
	if err := snap.ValidateAreaTide("east", "T2"); err != nil {
		t.Fatalf("expected valid pair, got %v", err)
	}
}

func TestValidateAreaTideRejectsWrongArea(t *testing.T) {
	err := snap.ValidateAreaTide("west", "T1")
	var apiErr *domain.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != domain.CodeAreaTideMismatch {
		t.Fatalf("expected AREA_TIDE_MISMATCH, got %v", err)
	}
}

func TestValidateAreaTideRejectsDisallowedTide(t *testing.T) {
	err := snap.ValidateAreaTide("east", "T9")
	var apiErr *domain.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != domain.CodeAreaTideMismatch {
		t.Fatalf("expected AREA_TIDE_MISMATCH, got %v", err)
	}
}

func TestCheckDigestRejectsStale(t *testing.T) {
	err := snap.CheckDigest("sha256:old")
	var apiErr *domain.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != domain.CodeStaleRuleDigest {
		t.Fatalf("expected STALE_RULE_DIGEST, got %v", err)
	}
	if err := snap.CheckDigest("sha256:abc"); err != nil {
		t.Fatalf("expected fresh digest accepted, got %v", err)
	}
}
