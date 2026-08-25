package adjudication

import "oyster-purification-release-gate/domain"

// RecheckScope names every dimension affected by an anomaly. A recheck batch
// must fully cover its scope: all affected cages, blind codes, timepoints and
// detection holes.
type RecheckScope struct {
	CageSeals  []string
	BlindCodes []string
	Timepoints []string
	Holes      []string
}

// Empty reports whether the scope names no affected dimension.
func (s RecheckScope) Empty() bool {
	return len(s.CageSeals) == 0 && len(s.BlindCodes) == 0 &&
		len(s.Timepoints) == 0 && len(s.Holes) == 0
}

// RecheckBatch is a single current-generation recheck for a task. Only one may
// exist per task generation; a unique constraint backs this invariant.
type RecheckBatch struct {
	TaskID     domain.TaskID
	Generation domain.Generation
	Scope      RecheckScope
}

// NewRecheckBatch builds a batch for the current generation. The caller is
// responsible for ensuring the scope is complete and non-empty before commit.
func NewRecheckBatch(taskID domain.TaskID, generation domain.Generation, scope RecheckScope) RecheckBatch {
	return RecheckBatch{TaskID: taskID, Generation: generation, Scope: scope}
}

// Validate enforces that a recheck batch has a non-empty, complete scope.
func (b RecheckBatch) Validate() error {
	if b.Scope.Empty() {
		return &domain.APIError{
			Code:    domain.CodeCoverageInvalid,
			Message: "recheck scope must name affected dimensions",
		}
	}
	return nil
}
