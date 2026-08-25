package task

import "oyster-purification-release-gate/domain"

// allowedTransitions is the single source of truth for the release state
// machine. Only forward edges are permitted; the three terminal states are
// entered exclusively through the terminal decision barrier.
var allowedTransitions = map[Status][]Status{
	StatusPendingLock:        {StatusPendingIntake},
	StatusPendingIntake:      {StatusSealingSamples},
	StatusSealingSamples:     {StatusResourcesBusy},
	StatusResourcesBusy:      {StatusVitalityCollecting},
	StatusVitalityCollecting: {StatusToxinVerifying},
	StatusToxinVerifying:     {StatusPathogenRetesting},
	StatusPathogenRetesting:  {StatusPendingReview},
	StatusPendingReview:      {StatusReleasable, StatusIsolated, StatusCancelled},
	StatusReleasable:         {StatusReleased, StatusIsolated, StatusCancelled},
}

// CanTransition reports whether moving from one status to another is allowed.
// Terminal states can never be left.
func CanTransition(from, to Status) bool {
	if from.IsTerminal() {
		return false
	}
	for _, n := range allowedTransitions[from] {
		if n == to {
			return true
		}
	}
	return false
}

// Next returns the single successor state, or false if from has no forward
// successor (for example a terminal state).
func Next(from Status) (Status, bool) {
	nexts := allowedTransitions[from]
	if len(nexts) == 0 {
		return "", false
	}
	return nexts[0], true
}

// MustTransition advances a status one step, returning a stable error when the
// edge is not permitted. It is used by commands that advance the aggregate
// after their own validation has passed.
func MustTransition(from, to Status) error {
	if !CanTransition(from, to) {
		return &domain.APIError{
			Code:    domain.CodeTerminalState,
			Message: "illegal state transition",
		}
	}
	return nil
}
