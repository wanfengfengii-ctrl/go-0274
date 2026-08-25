package ledger

import "oyster-purification-release-gate/domain"

// Sample is one of the three (triplicate) samples taken per cage. The ordinal
// distinguishes the three samples within a cage.
type Sample struct {
	ID       string
	CageSeal string
	Ordinal  int
}

// BlindCode maps a random blind code to a sample. Before reveal the mapping
// must never be exposed through responses, logs or error reasons.
type BlindCode struct {
	Code     string
	SampleID string
	Revealed bool
}

// Redacted returns the public, de-identified form of the blind code: the code
// itself but no sample mapping. It is the only representation safe to emit
// before an authorized reveal.
func (b BlindCode) Redacted() BlindCode {
	return BlindCode{Code: b.Code, Revealed: b.Revealed}
}

// Seal is the irreversible sealing record for one cage, binding the seal to a
// physical storage location.
type Seal struct {
	CageSeal string
	Location string
}

// ValidateTriplicate enforces the three-sample structure for a cage. A sealed
// cage must carry exactly three samples with distinct ordinals 0, 1 and 2.
func ValidateTriplicate(samples []Sample) error {
	if len(samples) != 3 {
		return &domain.APIError{
			Code:    domain.CodeCoverageInvalid,
			Message: "cage must carry exactly three samples",
		}
	}
	seen := make(map[int]bool)
	for _, s := range samples {
		if s.Ordinal < 0 || s.Ordinal > 2 || seen[s.Ordinal] {
			return &domain.APIError{
				Code:    domain.CodeCoverageInvalid,
				Message: "sample ordinals must be the distinct set {0,1,2}",
			}
		}
		seen[s.Ordinal] = true
	}
	return nil
}
