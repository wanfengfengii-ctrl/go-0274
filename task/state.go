// Package task implements the 牡蛎出池任务聚合 component: the release-task
// state machine, task generations, operation idempotency and the terminal
// single-write barrier.
package task

import "oyster-purification-release-gate/domain"

// Status is a release-task state in the documented state machine.
type Status string

const (
	StatusPendingLock        Status = "pending_lock"
	StatusPendingIntake      Status = "pending_intake"
	StatusSealingSamples     Status = "sealing_samples"
	StatusResourcesBusy      Status = "resources_busy"
	StatusVitalityCollecting Status = "vitality_collecting"
	StatusToxinVerifying     Status = "toxin_verifying"
	StatusPathogenRetesting  Status = "pathogen_retesting"
	StatusPendingReview      Status = "pending_review"
	StatusReleasable         Status = "releasable"
	StatusReleased           Status = "released"
	StatusIsolated           Status = "isolated"
	StatusCancelled          Status = "cancelled"
)

// IsTerminal reports whether the status is one of the three final states that
// are bound by the single terminal-decision record.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusReleased, StatusIsolated, StatusCancelled:
		return true
	default:
		return false
	}
}

// ValidateGeneration rejects a command whose generation does not match the
// task's current generation, producing STALE_GENERATION. This is the first
// guard every mutating command passes through.
func ValidateGeneration(current, submitted domain.Generation) error {
	if current != submitted {
		return &domain.APIError{
			Code:    domain.CodeStaleGeneration,
			Message: "stale task generation",
		}
	}
	return nil
}
