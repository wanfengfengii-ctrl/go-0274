package service

import (
	"context"

	"oyster-purification-release-gate/domain"
	"oyster-purification-release-gate/ledger"
	"oyster-purification-release-gate/store"
)

// GetTask returns the public task view. Blind codes are redacted so their
// sample mapping is never exposed before an authorized reveal.
func (s *Service) GetTask(ctx context.Context, id domain.TaskID) (*TaskResponse, error) {
	var out *TaskResponse
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		rec, err := s.loadTask(ctx, tx, id)
		if err != nil {
			return err
		}
		cages, err := tx.ListCages(ctx, id)
		if err != nil {
			return err
		}
		tps, err := tx.ListTimepoints(ctx, id)
		if err != nil {
			return err
		}
		leases, err := tx.ListLeasesByTask(ctx, id)
		if err != nil {
			return err
		}
		codes, err := tx.ListBlindCodesByTask(ctx, id)
		if err != nil {
			return err
		}
		resp := &TaskResponse{
			ID:          string(rec.ID),
			Status:      rec.Status,
			Generation:  rec.Generation,
			AreaID:      rec.AreaID,
			TideBatch:   rec.TideBatch,
			LockedCount: rec.LockedCount,
			CageSeals:   sealsOf(cages),
			Timepoints:  idsOfTP(tps),
		}
		for _, l := range leases {
			resp.Leases = append(resp.Leases, string(l.ResourceKey))
		}
		for _, c := range codes {
			resp.BlindCodes = append(resp.BlindCodes, c.Code)
		}
		out = resp
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetCoverage returns the vitality coverage state with a sorted missing list.
func (s *Service) GetCoverage(ctx context.Context, id domain.TaskID) (*CoverageResponse, error) {
	var out *CoverageResponse
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		rec, err := s.loadTask(ctx, tx, id)
		if err != nil {
			return err
		}
		cages, err := tx.ListCages(ctx, id)
		if err != nil {
			return err
		}
		tps, err := tx.ListTimepoints(ctx, id)
		if err != nil {
			return err
		}
		ops, err := tx.ListObservationPoints(ctx, id)
		if err != nil {
			return err
		}
		matrix := ledger.CoverageMatrix{
			CageSeals: sealsOf(cages), Timepoints: idsOfTP(tps), ObservationPoints: idsOfOP(ops),
		}
		cells, err := tx.ListVitality(ctx, id)
		if err != nil {
			return err
		}
		submitted := map[ledger.CoverageCell]bool{}
		for _, c := range cells {
			submitted[ledger.CoverageCell{CageSeal: c.CageSeal, TimepointID: c.TimepointID, ObservationPointID: c.ObservationPointID}] = true
		}
		missing := matrix.Missing(submitted)
		out = &CoverageResponse{
			Required: matrix.Required(),
			Recorded: len(submitted),
			Closed:   matrix.Closed(submitted),
		}
		for _, m := range missing {
			out.Missing = append(out.Missing, m.CageSeal+"/"+m.TimepointID+"/"+m.ObservationPointID)
		}
		_ = rec
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetEvidence aggregates the collected evidence counts.
func (s *Service) GetEvidence(ctx context.Context, id domain.TaskID) (*EvidenceResponse, error) {
	var out *EvidenceResponse
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		if _, err := s.loadTask(ctx, tx, id); err != nil {
			return err
		}
		v, err := tx.ListVitality(ctx, id)
		if err != nil {
			return err
		}
		w, err := tx.ListWater(ctx, id)
		if err != nil {
			return err
		}
		to, err := tx.ListToxin(ctx, id)
		if err != nil {
			return err
		}
		p, err := tx.ListPathogen(ctx, id)
		if err != nil {
			return err
		}
		rec, err := tx.GetTask(ctx, id)
		if err != nil {
			return err
		}
		reviews, err := tx.ListReviews(ctx, id, rec.Generation)
		if err != nil {
			return err
		}
		out = &EvidenceResponse{
			Vitality: len(v), Water: len(w), Toxin: len(to), Pathogen: len(p), Reviews: len(reviews),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetAudit returns the ordered audit trail for a task.
func (s *Service) GetAudit(ctx context.Context, id domain.TaskID) ([]AuditResponse, error) {
	var out []AuditResponse
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		if _, err := s.loadTask(ctx, tx, id); err != nil {
			return err
		}
		events, err := tx.ListAudit(ctx, id)
		if err != nil {
			return err
		}
		for _, e := range events {
			out = append(out, AuditResponse{Seq: e.Seq, Category: e.Category, Code: e.Code, Detail: e.Detail, At: e.At})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetPermit returns the release permit for a released task.
func (s *Service) GetPermit(ctx context.Context, id domain.TaskID) (*PermitResponse, error) {
	var out *PermitResponse
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		permit, err := tx.GetPermit(ctx, id)
		if err != nil {
			if store.IsNotFound(err) {
				return &domain.APIError{Code: "NOT_FOUND", Message: "no release permit for task"}
			}
			return err
		}
		out = &PermitResponse{PermitNumber: permit.PermitNumber, TaskID: string(permit.TaskID)}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Recover resumes any failed device calls whose retry time has arrived. It is
// the restart-recovery driver: after a crash between persisting a pending call
// and appending its result, this re-drives the instrument deterministically.
func (s *Service) Recover(ctx context.Context, now domain.LogicalTime) (int, error) {
	recovered, err := s.store.ScanRecovery(ctx, now)
	if err != nil {
		return 0, err
	}
	driven := 0
	for _, call := range recovered.PendingCalls {
		if _, err := s.runDeviceCall(ctx, call.ID, call.Attempts+1); err != nil {
			return driven, err
		}
		driven++
	}
	return driven, nil
}
