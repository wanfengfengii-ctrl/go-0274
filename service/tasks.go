package service

import (
	"context"
	"strings"

	"oyster-purification-release-gate/catalog"
	"oyster-purification-release-gate/domain"
	"oyster-purification-release-gate/store"
)

// leaseTTL is the logical duration a lock grants for every leased resource. It
// is far longer than any flow so leases are only ever released at the terminal
// transaction, satisfying the "终局前不得因设备失败提前释放" rule.
const leaseTTL domain.LogicalTime = 1 << 40

// CreateTask creates a pending-lock task, validating that the referenced rule
// snapshot exists and its digest is fresh. No lease is written yet.
func (s *Service) CreateTask(ctx context.Context, req CreateTaskRequest) (*TaskResponse, error) {
	if req.AreaID == "" || req.RuleSnapshotID == "" {
		return nil, &domain.APIError{Code: domain.CodeStaleRuleDigest, Message: "area and rule snapshot are required"}
	}
	snap, ok := s.catalog.Get(req.RuleSnapshotID)
	if !ok {
		return nil, &domain.APIError{Code: domain.CodeStaleRuleDigest, Message: "unknown rule snapshot"}
	}
	if snap.AreaID != req.AreaID {
		return nil, &domain.APIError{Code: domain.CodeAreaTideMismatch, Message: "harvest area does not match rule snapshot"}
	}

	id := domain.TaskID(newTaskID())
	rec := store.TaskRecord{
		ID:             id,
		Status:         "pending_lock",
		Generation:     1,
		AreaID:         req.AreaID,
		RuleSnapshotID: req.RuleSnapshotID,
		RuleDigest:     req.RuleDigest,
	}
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		if err := tx.InsertTask(ctx, rec); err != nil {
			return err
		}
		return audit(ctx, tx, id, "task", "CREATED", "task created pending lock")
	})
	if err != nil {
		return nil, err
	}
	return s.GetTask(ctx, id)
}

// LockTask atomically locks a pending task: it validates the rule digest and
// area/tide match, binds every cage, timepoint, observation point and blind
// code, and acquires every resource lease. Any failure rolls back completely.
func (s *Service) LockTask(ctx context.Context, id domain.TaskID, opID string, generation domain.Generation, req LockRequest) (*TaskResponse, error) {
	digest := requestDigest(req)
	if err := validateLockRequest(req); err != nil {
		return nil, err
	}

	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		rec, replay, err := s.gate(ctx, tx, id, generation, opID, digest)
		if err != nil {
			return err
		}
		if replay {
			return nil
		}
		if rec.Status != "pending_lock" {
			return &domain.APIError{Code: domain.CodeTerminalState, Message: "task is not pending lock"}
		}

		snap, ok := s.catalog.Resolve(rec.RuleSnapshotID, rec.RuleDigest)
		if !ok {
			return &domain.APIError{Code: domain.CodeStaleRuleDigest, Message: "unknown or stale rule snapshot"}
		}
		if err := snap.CheckDigest(rec.RuleDigest); err != nil {
			return err
		}
		if err := snap.ValidateAreaTide(rec.AreaID, catalog.TidePattern(req.TideBatch)); err != nil {
			return err
		}

		// Bind immutable dimensions.
		for _, seal := range req.CageSeals {
			if err := tx.InsertCage(ctx, store.CageRecord{TaskID: id, Seal: seal}); err != nil {
				return err
			}
		}
		for _, tp := range req.Timepoints {
			if err := tx.InsertTimepoint(ctx, store.TimepointRecord{TaskID: id, ID: tp}); err != nil {
				return err
			}
		}
		for _, op := range req.ObservationPoints {
			if err := tx.InsertObservationPoint(ctx, store.ObservationPointRecord{TaskID: id, ID: op}); err != nil {
				return err
			}
		}
		for _, code := range req.BlindCodes {
			if err := tx.InsertBlindCode(ctx, store.BlindCodeRecord{TaskID: id, Code: code}); err != nil {
				return err
			}
		}

		// Acquire every resource lease in the same transaction.
		now := tx.Now()
		leases := []store.LeaseRecord{
			{Kind: "pool", ResourceKey: req.Pool, TaskID: id, Generation: int64(generation), AcquiredAt: int64(now), ExpiresAt: int64(now + leaseTTL)},
			{Kind: "pump", ResourceKey: req.PumpBranch, TaskID: id, Generation: int64(generation), AcquiredAt: int64(now), ExpiresAt: int64(now + leaseTTL)},
			{Kind: "probe", ResourceKey: req.ProbeWindow, TaskID: id, Generation: int64(generation), AcquiredAt: int64(now), ExpiresAt: int64(now + leaseTTL)},
		}
		for _, h := range req.Holes {
			leases = append(leases, store.LeaseRecord{
				Kind: holeLeaseKind(h.Kind), ResourceKey: h.ID, TaskID: id, Generation: int64(generation),
				AcquiredAt: int64(now), ExpiresAt: int64(now + leaseTTL),
			})
		}
		for _, l := range leases {
			if err := tx.InsertLease(ctx, l); err != nil {
				if store.IsUniqueViolation(err) {
					return &domain.APIError{Code: domain.CodeResourceBusy, Message: "resource already leased: " + string(l.ResourceKey)}
				}
				return err
			}
		}

		// Persist the immutable snapshot fields on the task row.
		if _, err := tx.ExecContext(ctx, `UPDATE release_tasks
			SET tide_batch = ?, uv_lamp_batch = ?, locked_count = ?, reviewers = ?
			WHERE id = ?`, req.TideBatch, req.UVLampBatch, req.LockedCount, strings.Join(req.Reviewers, ","), id); err != nil {
			return err
		}
		if err := advance(ctx, tx, id, "pending_lock", "pending_intake"); err != nil {
			return err
		}
		if err := recordReceipt(ctx, tx, id, opID, digest, "locked"); err != nil {
			return err
		}
		return audit(ctx, tx, id, "task", "LOCKED", "task locked with immutable snapshot")
	})
	if err != nil {
		return nil, err
	}
	return s.GetTask(ctx, id)
}

// validateLockRequest rejects structurally invalid lock requests before any
// transaction opens, per the failure boundary "请求校验在业务事务前".
func validateLockRequest(req LockRequest) error {
	if req.TideBatch == "" || req.LockedCount <= 0 {
		return &domain.APIError{Code: domain.CodeCoverageInvalid, Message: "tide batch and positive locked count are required"}
	}
	if len(req.CageSeals) == 0 || len(req.Timepoints) == 0 || len(req.ObservationPoints) == 0 {
		return &domain.APIError{Code: domain.CodeCoverageInvalid, Message: "cages, timepoints and observation points are required"}
	}
	if req.Pool == "" || req.PumpBranch == "" || req.ProbeWindow == "" {
		return &domain.APIError{Code: domain.CodeCoverageInvalid, Message: "pool, pump branch and probe window are required"}
	}
	if len(req.Reviewers) < 2 {
		return &domain.APIError{Code: domain.CodeRoleConflict, Message: "at least two reviewers are required"}
	}
	seenSeal := map[string]bool{}
	for _, c := range req.CageSeals {
		if seenSeal[c] {
			return &domain.APIError{Code: domain.CodeDuplicateSeal, Message: "duplicate cage seal: " + c}
		}
		seenSeal[c] = true
	}
	seenCode := map[string]bool{}
	for _, c := range req.BlindCodes {
		if seenCode[c] {
			return &domain.APIError{Code: domain.CodeDuplicateBlindCode, Message: "duplicate blind code: " + c}
		}
		seenCode[c] = true
	}
	return nil
}
