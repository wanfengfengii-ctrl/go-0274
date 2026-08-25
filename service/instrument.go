package service

import (
	"context"
	"encoding/json"
	"strings"

	"oyster-purification-release-gate/adjudication"
	"oyster-purification-release-gate/catalog"
	"oyster-purification-release-gate/domain"
	"oyster-purification-release-gate/ledger"
	"oyster-purification-release-gate/store"
)

// holeLeaseKind maps a lock hole kind to its lease resource kind.
func holeLeaseKind(kind string) string {
	switch kind {
	case "toxin":
		return "toxin_hole"
	case "qpcr":
		return "qpcr_hole"
	case "culture":
		return "culture_hole"
	default:
		return kind
	}
}

// StartDeviceCall initiates a qPCR or incubator call for a hole. It persists a
// pending call, runs the instrument outside any transaction, then appends the
// result. Instrument failures are recorded as retryable, never as hard errors.
//
// Re-submitting a start for the same hole/generation does not re-drive the
// instrument: a finished call replays its stable result and a pending or
// failed call surfaces DEVICE_RETRY_PENDING so the caller resumes it through
// the retry endpoint, which computes the correct next attempt number. This
// keeps device_attempts' (call_id, attempt) uniqueness intact across retries.
func (s *Service) StartDeviceCall(ctx context.Context, id domain.TaskID, opID string, generation domain.Generation, req DeviceCallRequest) (*DeviceCallResponse, error) {
	if req.Kind != "qpcr" && req.Kind != "incubator" {
		return nil, &domain.APIError{Code: domain.CodeCoverageInvalid, Message: "device kind must be qpcr or incubator"}
	}

	var callID int64
	var existing *store.DeviceCallRecord
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		rec, replay, err := s.gate(ctx, tx, id, generation, opID, requestDigest(req))
		if err != nil {
			return err
		}
		if replay {
			return nil
		}
		if rec.Status != "pathogen_retesting" {
			return &domain.APIError{Code: domain.CodeTerminalState, Message: "task is not retesting pathogens"}
		}
		existing, err = tx.GetDeviceCall(ctx, id, req.Kind, req.Hole, int64(generation))
		if err == nil {
			callID = existing.ID
			return nil // already created; surface stable state below
		}
		if !store.IsNotFound(err) {
			return err
		}
		callID, err = tx.InsertDeviceCall(ctx, store.DeviceCallRecord{
			TaskID: id, Kind: req.Kind, Hole: req.Hole, Generation: int64(generation), Status: "pending", Attempts: 0,
		})
		if err != nil {
			return err
		}
		return recordReceipt(ctx, tx, id, opID, requestDigest(req), "device-call-started")
	})
	if err != nil {
		return nil, err
	}

	// A prior start already created this call: do not re-drive the instrument
	// at attempt 1, which would collide with the persisted device_attempts row
	// and surface as an internal error. Instead return the call's stable state.
	if existing != nil {
		resp := &DeviceCallResponse{CallID: callID, Hole: existing.Hole, Kind: existing.Kind}
		switch existing.Status {
		case "success":
			resp.Status = "success"
			return resp, nil
		default: // pending | failed
			return nil, &domain.APIError{
				Code:    domain.CodeDeviceRetryPending,
				Message: "device call already in progress for this hole; use the retry endpoint",
			}
		}
	}

	resp, err := s.runDeviceCall(ctx, callID, 1)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// runDeviceCall executes one attempt of a device call: it runs the adapter
// outside a transaction, then appends the result under a call-version guard.
func (s *Service) runDeviceCall(ctx context.Context, callID int64, attempt int) (*DeviceCallResponse, error) {
	// Load call metadata.
	var taskID domain.TaskID
	var kind, hole string
	var generation int64
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT task_id, kind, hole, generation FROM device_calls WHERE id = ?`, callID)
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			return store.ErrNoRows
		}
		return rows.Scan(&taskID, &kind, &hole, &generation)
	})
	if err != nil {
		return nil, err
	}

	// Run the instrument outside any transaction.
	req := adjudication.DeviceRequest{
		Kind:       adjudication.DeviceKind(kind),
		Hole:       hole,
		Generation: domain.Generation(generation),
		Attempt:    attempt,
	}
	payload, category, callErr := s.runner.Call(ctx, req)

	resp := &DeviceCallResponse{CallID: callID, Hole: hole, Kind: kind}
	err = s.store.Tx(ctx, func(tx *store.Tx) error {
		if callErr != nil {
			next := adjudication.NextRetryAfter(tx.Now(), attempt-1)
			if err := tx.InsertDeviceAttempt(ctx, store.DeviceAttemptRecord{
				CallID: callID, Attempt: attempt, Category: string(category), NextRetry: int64(next),
				Summary: callErr.Error(),
			}); err != nil {
				return err
			}
			if err := tx.UpdateDeviceCall(ctx, callID, "failed", attempt); err != nil {
				return err
			}
			resp.Status = "failed"
			return audit(ctx, tx, taskID, "device", "RETRY_PENDING", "device call "+kind+" failed: "+string(category))
		}
		// Success: append immutable pathogen evidence.
		ev, err := parsePathogen(kind, payload, s.defaultScales())
		if err != nil {
			if err := tx.InsertDeviceAttempt(ctx, store.DeviceAttemptRecord{
				CallID: callID, Attempt: attempt, Category: string(adjudication.FailureMalformed),
				NextRetry: int64(adjudication.NextRetryAfter(tx.Now(), attempt-1)), Summary: err.Error(),
			}); err != nil {
				return err
			}
			if err := tx.UpdateDeviceCall(ctx, callID, "failed", attempt); err != nil {
				return err
			}
			resp.Status = "failed"
			return audit(ctx, tx, taskID, "device", "MALFORMED", "device payload malformed")
		}
		ev.TaskID = taskID
		ev.Generation = generation
		ev.Hole = hole
		if err := tx.InsertPathogen(ctx, ev); err != nil {
			return err
		}
		if err := tx.InsertDeviceAttempt(ctx, store.DeviceAttemptRecord{
			CallID: callID, Attempt: attempt, Category: "", NextRetry: 0, Summary: "success",
		}); err != nil {
			return err
		}
		if err := tx.UpdateDeviceCall(ctx, callID, "success", attempt); err != nil {
			return err
		}
		resp.Status = "success"
		if err := audit(ctx, tx, taskID, "device", "SUCCESS", "pathogen evidence appended"); err != nil {
			return err
		}
		// Advance into review once every pathogen hole has evidence.
		closed, err := s.pathogenClosed(ctx, tx, taskID)
		if err != nil {
			return err
		}
		if closed {
			return advance(ctx, tx, taskID, "pathogen_retesting", "pending_review")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// RetryDeviceCall re-runs a previously failed device call.
func (s *Service) RetryDeviceCall(ctx context.Context, callID int64) (*DeviceCallResponse, error) {
	var attempts int
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		row := tx.QueryRowContext(ctx, `SELECT attempts, status FROM device_calls WHERE id = ?`, callID)
		var status string
		if err := row.Scan(&attempts, &status); err != nil {
			return err
		}
		if status == "success" {
			return &domain.APIError{Code: domain.CodeTerminalState, Message: "device call already succeeded"}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.runDeviceCall(ctx, callID, attempts+1)
}

// DeviceCallback records a late or normal device receipt. A receipt for a
// terminal task or stale generation becomes an audit-only rejection.
func (s *Service) DeviceCallback(ctx context.Context, req DeviceCallbackRequest) error {
	return s.store.Tx(ctx, func(tx *store.Tx) error {
		var taskID domain.TaskID
		var generation int64
		row := tx.QueryRowContext(ctx, `SELECT task_id, generation FROM device_calls WHERE id = ?`, req.CallID)
		if err := row.Scan(&taskID, &generation); err != nil {
			if store.IsNotFound(err) {
				return &domain.APIError{Code: "NOT_FOUND", Message: "device call not found"}
			}
			return err
		}
		rec, err := tx.GetTask(ctx, taskID)
		if err != nil {
			return err
		}
		if rec.Generation != generation || (rec.Status == "released" || rec.Status == "isolated" || rec.Status == "cancelled") {
			return audit(ctx, tx, taskID, "device", "LATE_REJECTED", "late device receipt rejected")
		}
		// Normal receipt already handled by StartDeviceCall; this path only
		// records the audit for a receipt that arrives outside a live call.
		return audit(ctx, tx, taskID, "device", "RECEIPT", "device receipt recorded")
	})
}

// parsePathogen decodes a device payload into immutable pathogen evidence.
func parsePathogen(kind string, payload []byte, sc catalog.Scales) (store.PathogenRecord, error) {
	if len(payload) == 0 {
		return store.PathogenRecord{}, &adjudication.DeviceError{Category: adjudication.FailureMalformed}
	}
	var p struct {
		NorovirusCt string `json:"norovirus_ct"`
		Coliform    string `json:"coliform"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return store.PathogenRecord{}, &adjudication.DeviceError{Category: adjudication.FailureMalformed}
	}
	ev := store.PathogenRecord{Kind: kind, Version: 1, Valid: true}
	switch kind {
	case "qpcr":
		ct, err := ledger.ParseScaled(p.NorovirusCt, sc.NorovirusDecimals, false)
		if err != nil {
			return store.PathogenRecord{}, &adjudication.DeviceError{Category: adjudication.FailureMalformed}
		}
		ev.NorovirusCt = ct
	case "incubator":
		coliform, err := ledger.ParseScaled(p.Coliform, sc.ColiformDecimals, false)
		if err != nil {
			return store.PathogenRecord{}, &adjudication.DeviceError{Category: adjudication.FailureMalformed}
		}
		ev.Coliform = coliform
	default:
		return store.PathogenRecord{}, &adjudication.DeviceError{Category: adjudication.FailureMalformed}
	}
	return ev, nil
}

func (s *Service) defaultScales() catalog.Scales { return catalog.DefaultScales() }

// pathogenClosed reports whether every qPCR and culture hole has evidence.
func (s *Service) pathogenClosed(ctx context.Context, tx *store.Tx, taskID domain.TaskID) (bool, error) {
	leases, err := tx.ListLeasesByTask(ctx, taskID)
	if err != nil {
		return false, err
	}
	pathogenHoles := map[string]bool{}
	for _, l := range leases {
		if l.Kind == "qpcr_hole" || l.Kind == "culture_hole" {
			pathogenHoles[l.ResourceKey] = true
		}
	}
	if len(pathogenHoles) == 0 {
		return true, nil
	}
	evidence, err := tx.ListPathogen(ctx, taskID)
	if err != nil {
		return false, err
	}
	seen := map[string]bool{}
	for _, e := range evidence {
		seen[e.Hole] = true
	}
	for h := range pathogenHoles {
		if !seen[h] {
			return false, nil
		}
	}
	return true, nil
}

// CreateRecheck records the single current-generation recheck batch and bumps
// the task generation so new evidence is appended under a fresh generation
// without overwriting old evidence.
func (s *Service) CreateRecheck(ctx context.Context, id domain.TaskID, opID string, generation domain.Generation, req RecheckRequest) error {
	digest := requestDigest(req)
	return s.store.Tx(ctx, func(tx *store.Tx) error {
		rec, err := s.loadTask(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := checkGeneration(rec, generation); err != nil {
			return err
		}
		replay, err := idempotencyGate(ctx, tx, opID, digest)
		if err != nil {
			return err
		}
		if replay {
			return nil
		}
		scope := adjudication.RecheckScope{
			CageSeals: req.CageSeals, BlindCodes: req.BlindCodes, Timepoints: req.Timepoints, Holes: req.Holes,
		}
		batch := adjudication.NewRecheckBatch(id, generation, scope)
		if err := batch.Validate(); err != nil {
			return err
		}
		scopeJSON, _ := json.Marshal(scope)
		if err := tx.InsertRecheck(ctx, store.RecheckRecord{
			TaskID: id, Generation: int64(generation), ScopeJSON: string(scopeJSON),
		}); err != nil {
			if store.IsUniqueViolation(err) {
				return &domain.APIError{Code: domain.CodeCoverageInvalid, Message: "a recheck already exists for this generation"}
			}
			return err
		}
		if err := tx.SetTaskGeneration(ctx, id, int64(generation)+1); err != nil {
			return err
		}
		if err := recordReceipt(ctx, tx, id, opID, digest, "recheck-created"); err != nil {
			return err
		}
		return audit(ctx, tx, id, "recheck", "CREATED", "current-generation recheck batch created: "+req.Reason)
	})
}

// SubmitReview records one independent review and, once two distinct approving
// reviews exist, advances the task to releasable.
func (s *Service) SubmitReview(ctx context.Context, id domain.TaskID, opID string, generation domain.Generation, req ReviewRequest) error {
	digest := requestDigest(req)
	return s.store.Tx(ctx, func(tx *store.Tx) error {
		rec, replay, err := s.gate(ctx, tx, id, generation, opID, digest)
		if err != nil {
			return err
		}
		if replay {
			return nil
		}
		if rec.Status != "pending_review" {
			return &domain.APIError{Code: domain.CodeTerminalState, Message: "task is not pending review"}
		}
		if err := s.validateReviewerInList(rec, req.Reviewer); err != nil {
			return err
		}
		if err := tx.InsertReview(ctx, store.ReviewRecord{
			TaskID: id, Generation: int64(generation), Reviewer: req.Reviewer, Approved: req.Approved,
		}); err != nil {
			return err
		}
		reviews, err := tx.ListReviews(ctx, id, int64(generation))
		if err != nil {
			return err
		}
		// Once two distinct approving reviews exist, enforce the full pair rule
		// (distinct, qualified, no overlap with intake confirmers) and advance.
		if len(reviews) >= 2 {
			if err := s.validateReviewPair(ctx, tx, rec, reviews); err != nil {
				return err
			}
			if err := advance(ctx, tx, id, "pending_review", "releasable"); err != nil {
				return err
			}
		}
		if err := recordReceipt(ctx, tx, id, opID, digest, "reviewed"); err != nil {
			return err
		}
		return audit(ctx, tx, id, "review", "REVIEWED", "review recorded by "+req.Reviewer)
	})
}

// validateReviewerInList enforces that a reviewer is in the locked reviewer list.
func (s *Service) validateReviewerInList(rec *store.TaskRecord, reviewer string) error {
	for _, r := range strings.Split(rec.Reviewers, ",") {
		if r == reviewer {
			return nil
		}
	}
	return &domain.APIError{Code: domain.CodeRoleConflict, Message: "reviewer is not in the locked reviewer list"}
}

// validateReviewPair applies the catalog and adjudication reviewer rules to the
// collected reviews: at least two distinct approving reviewers, both qualified,
// and neither overlapping with an intake confirmer.
func (s *Service) validateReviewPair(ctx context.Context, tx *store.Tx, rec *store.TaskRecord, records []store.ReviewRecord) error {
	reviews := make([]adjudication.Review, len(records))
	for i, r := range records {
		reviews[i] = adjudication.Review{TaskID: r.TaskID, Generation: domain.Generation(r.Generation), Reviewer: domain.PersonID(r.Reviewer), Approved: r.Approved}
	}
	if err := adjudication.ValidateReviews(reviews); err != nil {
		return err
	}
	confirmers, err := tx.ListPersonActions(ctx, rec.ID, string(catalog.RoleIntakeConfirmer))
	if err != nil {
		return err
	}
	persons := make([]catalog.Person, 0, len(records))
	for _, r := range records {
		persons = append(persons, catalog.Person{ID: domain.PersonID(r.Reviewer), Roles: []catalog.Role{catalog.RoleReviewer}})
	}
	confPerson := make([]catalog.Person, 0, len(confirmers))
	for _, c := range confirmers {
		confPerson = append(confPerson, catalog.Person{ID: domain.PersonID(c.PersonID), Roles: []catalog.Role{catalog.RoleIntakeConfirmer}})
	}
	if len(persons) < 2 {
		return &domain.APIError{Code: domain.CodeRoleConflict, Message: "two distinct reviewers are required"}
	}
	return catalog.ValidateReviewers(persons[0], persons[1], confPerson...)
}

// RevealBlindCode authorizes unmasking a blind code and returns its sample
// mapping, which is never exposed before this call.
func (s *Service) RevealBlindCode(ctx context.Context, opID string, req RevealRequest) (string, error) {
	var sampleID string
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		bc, err := tx.GetBlindCode(ctx, req.Code)
		if err != nil {
			if store.IsNotFound(err) {
				return &domain.APIError{Code: "NOT_FOUND", Message: "blind code not found"}
			}
			return err
		}
		if err := tx.RevealBlindCode(ctx, req.Code); err != nil {
			return err
		}
		sampleID = bc.SampleID
		return audit(ctx, tx, bc.TaskID, "blind", "REVEALED", "blind code revealed: "+req.Code)
	})
	if err != nil {
		return "", err
	}
	return sampleID, nil
}
