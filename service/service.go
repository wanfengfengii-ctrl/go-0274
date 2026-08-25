package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"oyster-purification-release-gate/adjudication"
	"oyster-purification-release-gate/catalog"
	"oyster-purification-release-gate/domain"
	"oyster-purification-release-gate/store"
	"oyster-purification-release-gate/task"
)

// Service orchestrates the release flow across catalog rules, task state,
// ledger evidence and adjudication, all persisted through store. It owns no
// state beyond its dependencies; every command is deterministic.
type Service struct {
	store   *store.Store
	catalog *catalog.Catalog
	runner  adjudication.Runner
}

// New assembles a Service. adapter is the instrument boundary; tests inject a
// scripted adapter while the server injects a static healthy one.
func New(st *store.Store, cat *catalog.Catalog, adapter adjudication.DeviceAdapter) *Service {
	return &Service{store: st, catalog: cat, runner: adjudication.Runner{Adapter: adapter}}
}

// Store exposes the underlying store for startup wiring and recovery.
func (s *Service) Store() *store.Store { return s.store }

// requestDigest computes the normalized content digest of a request body. The
// operation id and generation travel in headers, so only the body is hashed.
func requestDigest(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// idempotencyGate enforces the operation-id replay rule against the persisted
// receipt table. It returns replay=true when this exact content already ran.
func idempotencyGate(ctx context.Context, tx *store.Tx, opID, digest string) (bool, error) {
	prev, err := tx.GetReceipt(ctx, opID)
	if err == nil {
		if prev.Digest == digest {
			return true, nil
		}
		return false, &domain.APIError{
			Code:    domain.CodeOperationConflict,
			Message: "operation id reused with different content",
		}
	}
	if !store.IsNotFound(err) {
		return false, err
	}
	return false, nil
}

// recordReceipt persists the operation result inside the same transaction as
// the business write so a failed command leaves no receipt behind.
func recordReceipt(ctx context.Context, tx *store.Tx, taskID domain.TaskID, opID, digest, result string) error {
	return tx.InsertReceipt(ctx, store.ReceiptRecord{
		OperationID: opID,
		Digest:      digest,
		Result:      result,
		TaskID:      taskID,
	})
}

// loadTask loads a task, translating absence to a stable not-found error.
func (s *Service) loadTask(ctx context.Context, tx *store.Tx, id domain.TaskID) (*store.TaskRecord, error) {
	rec, err := tx.GetTask(ctx, id)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, &domain.APIError{Code: "NOT_FOUND", Message: "task not found"}
		}
		return nil, err
	}
	return rec, nil
}

// checkGeneration enforces the generation guard and the terminal-state guard in
// one place. Every mutating command calls this first.
func checkGeneration(txRec *store.TaskRecord, submitted domain.Generation) error {
	if txRec.Status == "released" || txRec.Status == "isolated" || txRec.Status == "cancelled" {
		return &domain.APIError{Code: domain.CodeTerminalState, Message: "task is in a terminal state"}
	}
	if txRec.Generation != int64(submitted) {
		return &domain.APIError{Code: domain.CodeStaleGeneration, Message: "stale task generation"}
	}
	return nil
}

// gate is the common preamble for every mutating command: it loads the task,
// enforces the generation guard and checks the idempotency receipt, returning
// whether this operation is a replay. Status-specific checks follow the gate so
// a replay of an already-completed operation never hits a stale status guard.
func (s *Service) gate(ctx context.Context, tx *store.Tx, id domain.TaskID, generation domain.Generation, opID, digest string) (*store.TaskRecord, bool, error) {
	rec, err := s.loadTask(ctx, tx, id)
	if err != nil {
		return nil, false, err
	}
	if err := checkGeneration(rec, generation); err != nil {
		return nil, false, err
	}
	replay, err := idempotencyGate(ctx, tx, opID, digest)
	if err != nil {
		return nil, false, err
	}
	return rec, replay, nil
}

// advance validates a state transition against the task state machine and then
// applies it with a conditional update, surfacing a stable error otherwise.
func advance(ctx context.Context, tx *store.Tx, id domain.TaskID, from, to string) error {
	if err := task.MustTransition(task.Status(from), task.Status(to)); err != nil {
		return err
	}
	ok, err := tx.UpdateTaskStatus(ctx, id, from, to)
	if err != nil {
		return err
	}
	if !ok {
		return &domain.APIError{
			Code:    domain.CodeTerminalState,
			Message: "task is not in the expected state",
		}
	}
	return nil
}

// audit appends an audit event inside the current transaction.
func audit(ctx context.Context, tx *store.Tx, taskID domain.TaskID, category, code, detail string) error {
	return tx.InsertAudit(ctx, store.AuditRecord{
		TaskID:   taskID,
		Category: category,
		Code:     code,
		Detail:   detail,
		At:       int64(tx.Now()),
	})
}
