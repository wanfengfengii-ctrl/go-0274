package adjudication

import "oyster-purification-release-gate/domain"

// PathogenEvidence is the immutable result of a qPCR (norovirus Ct) or
// incubator (coliform) run. Values are scaled integers; Ct uses one decimal
// place, coliform zero. A malformed instrument response never reaches this
// type.
type PathogenEvidence struct {
	TaskID      domain.TaskID
	Generation  domain.Generation
	Hole        string
	Kind        DeviceKind
	NorovirusCt int64
	Coliform    int64
	Valid       bool
}

// Validate enforces the measurement invariants: a norovirus Ct must be
// non-negative, coliform must be non-negative. It never mutates Valid unless
// the evidence is well-formed.
func (p *PathogenEvidence) Validate() error {
	if p.NorovirusCt < 0 {
		return &domain.APIError{
			Code:    domain.CodeCoverageInvalid,
			Message: "norovirus Ct must be non-negative",
		}
	}
	if p.Coliform < 0 {
		return &domain.APIError{
			Code:    domain.CodeCoverageInvalid,
			Message: "coliform count must be non-negative",
		}
	}
	return nil
}

// Exceeds reports whether the evidence breaches a threshold. Threshold
// comparison is integer-only.
func (p PathogenEvidence) Exceeds(norovirusMax, coliformMax int64) bool {
	return p.NorovirusCt > norovirusMax || p.Coliform > coliformMax
}
