package task

import "oyster-purification-release-gate/domain"

// Decision identifies the single terminal outcome. Release, isolate and cancel
// all race through the same single-write barrier, so at most one wins.
type Decision string

const (
	DecisionRelease Decision = "release"
	DecisionIsolate Decision = "isolate"
	DecisionCancel  Decision = "cancel"
)

// TerminalDecision is the persistent record of the single winning outcome. Its
// task_id is unique, which is what guarantees a second decision cannot be
// committed even under concurrent delivery.
type TerminalDecision struct {
	TaskID   domain.TaskID
	Decision Decision
	Reason   string
	WinnerOp domain.OperationID
	Version  int64
}

// AttemptTerminal implements the terminal single-write barrier in pure logic.
// If a decision already exists the call is rejected with TERMINAL_STATE; the
// database unique constraint backs this up so the check and the write cannot
// be raced. It returns the proposed decision and nil when this caller may win.
func AttemptTerminal(existing *TerminalDecision, proposed TerminalDecision) (*TerminalDecision, error) {
	if existing != nil {
		return nil, &domain.APIError{
			Code:    domain.CodeTerminalState,
			Message: "task already has a terminal decision",
		}
	}
	return &proposed, nil
}

// DecisionFromString parses a terminal decision identifier.
func DecisionFromString(s string) (Decision, bool) {
	switch Decision(s) {
	case DecisionRelease, DecisionIsolate, DecisionCancel:
		return Decision(s), true
	default:
		return "", false
	}
}
