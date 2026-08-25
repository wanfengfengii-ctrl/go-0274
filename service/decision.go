package service

import (
	"context"

	"oyster-purification-release-gate/adjudication"
	"oyster-purification-release-gate/domain"
	"oyster-purification-release-gate/store"
	"oyster-purification-release-gate/task"
)

// Decide performs a terminal decision (release, isolate or cancel). The three
// outcomes race through the same single-write barrier: exactly one caller wins
// the terminal record, every lease is released only inside that transaction,
// and release additionally mints the unique permit.
func (s *Service) Decide(ctx context.Context, id domain.TaskID, opID string, generation domain.Generation, decision string, req DecisionRequest) (*DecisionResponse, error) {
	d, ok := task.DecisionFromString(decision)
	if !ok {
		return nil, &domain.APIError{Code: domain.CodeCoverageInvalid, Message: "unknown decision"}
	}
	digest := requestDigest(req)
	var out *DecisionResponse
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		rec, replay, err := s.gate(ctx, tx, id, generation, opID, digest)
		if err != nil {
			return err
		}
		if replay {
			out = &DecisionResponse{Decision: string(d), Reason: req.Reason}
			return nil
		}
		// Precondition: release requires a fully reviewed releasable task;
		// isolate and cancel are also allowed at pending_review.
		if d == task.DecisionRelease && rec.Status != "releasable" {
			return &domain.APIError{Code: domain.CodeTerminalState, Message: "task is not releasable"}
		}
		if d != task.DecisionRelease && rec.Status != "releasable" && rec.Status != "pending_review" {
			return &domain.APIError{Code: domain.CodeTerminalState, Message: "task is not ready for a terminal decision"}
		}
		if _, err := tx.GetTerminal(ctx, id); err == nil {
			return &domain.APIError{Code: domain.CodeTerminalState, Message: "task already has a terminal decision"}
		} else if !store.IsNotFound(err) {
			return err
		}

		proposed := task.TerminalDecision{
			TaskID: id, Decision: d, Reason: req.Reason, WinnerOp: domain.OperationID(opID), Version: 1,
		}
		if _, err := task.AttemptTerminal(nil, proposed); err != nil {
			return err
		}
		if err := tx.InsertTerminal(ctx, store.TerminalRecord{
			TaskID: id, Decision: string(d), Reason: req.Reason, WinnerOp: opID, Version: 1,
		}); err != nil {
			if store.IsUniqueViolation(err) {
				return &domain.APIError{Code: domain.CodeTerminalState, Message: "task already has a terminal decision"}
			}
			return err
		}

		if d == task.DecisionRelease {
			if err := tx.InsertPermit(ctx, store.PermitRecord{
				PermitNumber: adjudication.PermitNumberFor(id, 1), TaskID: id, ReleasedAt: int64(tx.Now()),
			}); err != nil {
				return err
			}
		}

		// Release every lease only now, inside the winning terminal transaction.
		if err := tx.DeleteLeasesByTask(ctx, id); err != nil {
			return err
		}

		target := map[task.Decision]string{
			task.DecisionRelease: "released",
			task.DecisionIsolate: "isolated",
			task.DecisionCancel:  "cancelled",
		}[d]
		if err := advance(ctx, tx, id, rec.Status, target); err != nil {
			return err
		}
		if err := recordReceipt(ctx, tx, id, opID, digest, string(d)); err != nil {
			return err
		}
		if err := audit(ctx, tx, id, "decision", string(d), "terminal decision recorded: "+req.Reason); err != nil {
			return err
		}
		out = &DecisionResponse{Decision: string(d), Reason: req.Reason}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Dispatch advances a releasable task to released, generating the unique
// release permit. It shares the release path with the release decision so that
// either entry point yields exactly one permit and one released state.
func (s *Service) Dispatch(ctx context.Context, id domain.TaskID, opID string, generation domain.Generation) (*PermitResponse, error) {
	_, err := s.Decide(ctx, id, opID, generation, string(task.DecisionRelease), DecisionRequest{Reason: "dispatched"})
	if err != nil {
		return nil, err
	}
	permit, err := s.GetPermit(ctx, id)
	if err != nil {
		return nil, err
	}
	return permit, nil
}
