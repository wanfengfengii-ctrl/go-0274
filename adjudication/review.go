package adjudication

import (
	"fmt"

	"oyster-purification-release-gate/domain"
)

// Review is one independent reviewer's conclusion for a task generation.
type Review struct {
	TaskID     domain.TaskID
	Generation domain.Generation
	Reviewer   domain.PersonID
	Approved   bool
}

// ReleasePermit is the unique dispatch credential generated for a released
// task. Its number is unique per task; a database unique constraint guarantees
// a second permit cannot be produced.
type ReleasePermit struct {
	PermitNumber string
	TaskID       domain.TaskID
	ReleasedAt   domain.LogicalTime
}

// ValidateReviews enforces the two-reviewer closure rule at the value level: at
// least two reviews must exist, they must come from two distinct reviewers, and
// both must approve. Role qualification and non-overlap with intake confirmers
// are enforced earlier by the catalog personnel rules.
func ValidateReviews(reviews []Review) error {
	if len(reviews) < 2 {
		return &domain.APIError{
			Code:    domain.CodeCoverageInvalid,
			Message: "at least two independent reviews are required",
		}
	}
	seen := make(map[domain.PersonID]bool)
	for _, r := range reviews {
		if !r.Approved {
			return &domain.APIError{
				Code:    domain.CodeCoverageInvalid,
				Message: "a reviewer did not approve the release",
			}
		}
		if seen[r.Reviewer] {
			return &domain.APIError{
				Code:    domain.CodeRoleConflict,
				Message: "reviews must come from distinct reviewers",
			}
		}
		seen[r.Reviewer] = true
	}
	return nil
}

// PermitNumberFor derives the unique permit number from the task and a
// monotonic sequence. It is deterministic so the same task/sequence always
// yields the same number, making the release idempotent.
func PermitNumberFor(taskID domain.TaskID, seq int64) string {
	return fmt.Sprintf("PERMIT-%s-%04d", taskID, seq)
}
