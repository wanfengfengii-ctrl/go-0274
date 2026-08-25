package service

import (
	"context"

	"oyster-purification-release-gate/catalog"
	"oyster-purification-release-gate/domain"
	"oyster-purification-release-gate/ledger"
	"oyster-purification-release-gate/store"
)

// snapshotFor resolves the immutable rule snapshot a task locked onto.
func (s *Service) snapshotFor(rec *store.TaskRecord) (catalog.RuleSnapshot, bool) {
	return s.catalog.Resolve(rec.RuleSnapshotID, rec.RuleDigest)
}

// ConfirmIntake records a two-person intake confirmation and advances the task
// from pending_intake to sealing_samples.
func (s *Service) ConfirmIntake(ctx context.Context, id domain.TaskID, opID string, generation domain.Generation, req IntakeRequest) error {
	digest := requestDigest(req)
	return s.store.Tx(ctx, func(tx *store.Tx) error {
		rec, replay, err := s.gate(ctx, tx, id, generation, opID, digest)
		if err != nil {
			return err
		}
		if replay {
			return nil
		}
		if rec.Status != "pending_intake" {
			return &domain.APIError{Code: domain.CodeTerminalState, Message: "task is not pending intake confirmation"}
		}
		if len(req.Confirmers) != 2 {
			return &domain.APIError{Code: domain.CodeRoleConflict, Message: "exactly two intake confirmers are required"}
		}
		a := catalog.Person{ID: domain.PersonID(req.Confirmers[0]), Roles: []catalog.Role{catalog.RoleIntakeConfirmer}}
		b := catalog.Person{ID: domain.PersonID(req.Confirmers[1]), Roles: []catalog.Role{catalog.RoleIntakeConfirmer}}
		if err := catalog.ValidateIntakeConfirmers(a, b); err != nil {
			return err
		}
		for _, p := range []catalog.Person{a, b} {
			if err := tx.InsertPersonAction(ctx, store.PersonActionRecord{
				TaskID: id, PersonID: string(p.ID), Role: string(catalog.RoleIntakeConfirmer), Generation: int64(generation),
			}); err != nil {
				return err
			}
		}
		if err := advance(ctx, tx, id, "pending_intake", "sealing_samples"); err != nil {
			return err
		}
		if err := recordReceipt(ctx, tx, id, opID, digest, "confirmed"); err != nil {
			return err
		}
		return audit(ctx, tx, id, "intake", "CONFIRMED", "two-person intake confirmation recorded")
	})
}

// SealSamples registers the triplicate samples, binds blind codes and records
// seals, advancing sealing_samples to resources_busy.
func (s *Service) SealSamples(ctx context.Context, id domain.TaskID, opID string, generation domain.Generation, req SealRequest) error {
	digest := requestDigest(req)
	return s.store.Tx(ctx, func(tx *store.Tx) error {
		rec, replay, err := s.gate(ctx, tx, id, generation, opID, digest)
		if err != nil {
			return err
		}
		if replay {
			return nil
		}
		if rec.Status != "sealing_samples" {
			return &domain.APIError{Code: domain.CodeTerminalState, Message: "task is not sealing samples"}
		}
		if len(req.Seals) == 0 {
			return &domain.APIError{Code: domain.CodeCoverageInvalid, Message: "at least one seal is required"}
		}
		for _, spec := range req.Seals {
			samples := make([]ledger.Sample, 0, len(spec.BlindCodes))
			for i, code := range spec.BlindCodes {
				samples = append(samples, ledger.Sample{ID: string(id) + "-" + spec.CageSeal + "-" + string(rune('0'+i)), CageSeal: spec.CageSeal, Ordinal: i})
				sampleID := samples[i].ID
				if err := tx.InsertSample(ctx, store.SampleRecord{ID: sampleID, TaskID: id, CageSeal: spec.CageSeal, Ordinal: i}); err != nil {
					return err
				}
				bc, err := tx.GetBlindCode(ctx, code)
				if err != nil {
					return &domain.APIError{Code: domain.CodeDuplicateBlindCode, Message: "unknown blind code: " + code}
				}
				// The code must belong to this task. A cross-batch mis-scan of
				// another open task's blind code is rejected here rather than
				// silently ignored; otherwise this task advances on an unbound
				// code and the sample mapping breaks at reveal.
				if bc.TaskID != id {
					return &domain.APIError{Code: domain.CodeDuplicateBlindCode, Message: "blind code belongs to another task: " + code}
				}
				if bc.SampleID != "" {
					return &domain.APIError{Code: domain.CodeDuplicateBlindCode, Message: "blind code already bound: " + code}
				}
				// Claim the code with a conditional update. Exactly one row must
				// move; zero means a concurrent binding won it and this task must
				// not proceed to resources_busy on a code it never bound.
				res, err := tx.ExecContext(ctx, `UPDATE blind_codes SET sample_id = ? WHERE code = ? AND task_id = ? AND sample_id = ''`,
					sampleID, code, id)
				if err != nil {
					return err
				}
				n, err := res.RowsAffected()
				if err != nil {
					return err
				}
				if n != 1 {
					return &domain.APIError{Code: domain.CodeDuplicateBlindCode, Message: "blind code already bound: " + code}
				}
			}
			if err := ledger.ValidateTriplicate(samples); err != nil {
				return err
			}
			if err := tx.InsertSeal(ctx, store.SealRecord{TaskID: id, CageSeal: spec.CageSeal, Location: spec.Location}); err != nil {
				return err
			}
		}
		if err := advance(ctx, tx, id, "sealing_samples", "resources_busy"); err != nil {
			return err
		}
		if err := recordReceipt(ctx, tx, id, opID, digest, "sealed"); err != nil {
			return err
		}
		return audit(ctx, tx, id, "samples", "SEALED", "samples sealed and blind codes bound")
	})
}

// SubmitVitality records vitality cells and, once the coverage matrix closes,
// advances the task into toxin verification.
func (s *Service) SubmitVitality(ctx context.Context, id domain.TaskID, opID string, generation domain.Generation, req VitalityRequest) error {
	digest := requestDigest(req)
	return s.store.Tx(ctx, func(tx *store.Tx) error {
		rec, replay, err := s.gate(ctx, tx, id, generation, opID, digest)
		if err != nil {
			return err
		}
		if replay {
			return nil
		}
		if rec.Status != "resources_busy" && rec.Status != "vitality_collecting" {
			return &domain.APIError{Code: domain.CodeTerminalState, Message: "task is not collecting vitality"}
		}
		for _, cell := range req.Cells {
			v := ledger.VitalityCounts{Closed: cell.Closed, WeakOpen: cell.WeakOpen, Dead: cell.Dead, Broken: cell.Broken}
			if err := v.ValidateConservation(rec.LockedCount); err != nil {
				return err
			}
			existing, err := tx.GetVitalityCell(ctx, id, cell.CageSeal, cell.TimepointID, cell.ObservationPointID)
			if err == nil {
				if existing.Closed != cell.Closed || existing.WeakOpen != cell.WeakOpen ||
					existing.Dead != cell.Dead || existing.Broken != cell.Broken {
					return &domain.APIError{Code: domain.CodeCoverageInvalid, Message: "cell already recorded with different content"}
				}
				continue // idempotent same content
			}
			if !store.IsNotFound(err) {
				return err
			}
			if err := tx.InsertVitality(ctx, store.VitalityRecord{
				TaskID: id, CageSeal: cell.CageSeal, TimepointID: cell.TimepointID,
				ObservationPointID: cell.ObservationPointID, Closed: cell.Closed, WeakOpen: cell.WeakOpen,
				Dead: cell.Dead, Broken: cell.Broken, SupplementOf: cell.SupplementOf, Version: 1,
			}); err != nil {
				return err
			}
		}
		if err := s.evaluateMortality(ctx, tx, rec, req.Cells); err != nil {
			return err
		}
		if rec.Status == "resources_busy" {
			if err := advance(ctx, tx, id, "resources_busy", "vitality_collecting"); err != nil {
				return err
			}
		}
		closed, err := s.vitalityClosed(ctx, tx, id)
		if err != nil {
			return err
		}
		if closed {
			if err := advance(ctx, tx, id, "vitality_collecting", "toxin_verifying"); err != nil {
				return err
			}
		}
		if err := recordReceipt(ctx, tx, id, opID, digest, "vitality"); err != nil {
			return err
		}
		return audit(ctx, tx, id, "evidence", "VITALITY", "vitality cells recorded")
	})
}

// vitalityClosed reports whether every required coverage cell is recorded.
func (s *Service) vitalityClosed(ctx context.Context, tx *store.Tx, id domain.TaskID) (bool, error) {
	cages, err := tx.ListCages(ctx, id)
	if err != nil {
		return false, err
	}
	tps, err := tx.ListTimepoints(ctx, id)
	if err != nil {
		return false, err
	}
	ops, err := tx.ListObservationPoints(ctx, id)
	if err != nil {
		return false, err
	}
	matrix := ledger.CoverageMatrix{
		CageSeals:         sealsOf(cages),
		Timepoints:        idsOfTP(tps),
		ObservationPoints: idsOfOP(ops),
	}
	submitted := map[ledger.CoverageCell]bool{}
	cells, err := tx.ListVitality(ctx, id)
	if err != nil {
		return false, err
	}
	for _, c := range cells {
		submitted[ledger.CoverageCell{CageSeal: c.CageSeal, TimepointID: c.TimepointID, ObservationPointID: c.ObservationPointID}] = true
	}
	return matrix.Closed(submitted), nil
}

// SubmitWater records fixed-point water-quality readings.
func (s *Service) SubmitWater(ctx context.Context, id domain.TaskID, opID string, generation domain.Generation, req WaterRequest) error {
	digest := requestDigest(req)
	return s.store.Tx(ctx, func(tx *store.Tx) error {
		rec, replay, err := s.gate(ctx, tx, id, generation, opID, digest)
		if err != nil {
			return err
		}
		if replay {
			return nil
		}
		if rec.Status != "resources_busy" && rec.Status != "vitality_collecting" {
			return &domain.APIError{Code: domain.CodeTerminalState, Message: "task is not collecting vitality"}
		}
		snap, _ := s.snapshotFor(rec)
		for _, w := range req.Readings {
			wr, err := parseWater(w, snap.Scales)
			if err != nil {
				return err
			}
			wr.TaskID = id
			if err := tx.InsertWater(ctx, wr); err != nil {
				return err
			}
		}
		if rec.Status == "resources_busy" {
			if err := advance(ctx, tx, id, "resources_busy", "vitality_collecting"); err != nil {
				return err
			}
		}
		if err := recordReceipt(ctx, tx, id, opID, digest, "water"); err != nil {
			return err
		}
		return audit(ctx, tx, id, "evidence", "WATER", "water readings recorded")
	})
}

// SubmitToxin records plate-reader toxin evidence, deriving concentrations in
// integer arithmetic, and advances into pathogen retesting once every toxin
// hole has a reading.
func (s *Service) SubmitToxin(ctx context.Context, id domain.TaskID, opID string, generation domain.Generation, req ToxinRequest) error {
	digest := requestDigest(req)
	return s.store.Tx(ctx, func(tx *store.Tx) error {
		rec, replay, err := s.gate(ctx, tx, id, generation, opID, digest)
		if err != nil {
			return err
		}
		if replay {
			return nil
		}
		if rec.Status != "toxin_verifying" {
			return &domain.APIError{Code: domain.CodeTerminalState, Message: "task is not verifying toxins"}
		}
		snap, ok := s.snapshotFor(rec)
		if !ok {
			return &domain.APIError{Code: domain.CodeStaleRuleDigest, Message: "unknown rule snapshot"}
		}
		for _, t := range req.Readings {
			tr, err := parseToxin(t, snap.Scales)
			if err != nil {
				return err
			}
			tr.TaskID = id
			if err := tx.InsertToxin(ctx, tr); err != nil {
				return err
			}
			if err := s.evaluateToxin(ctx, tx, id, snap, tr); err != nil {
				return err
			}
		}
		// Advance once every toxin hole has a reading.
		closed, err := s.toxinClosed(ctx, tx, id)
		if err != nil {
			return err
		}
		if closed {
			if err := advance(ctx, tx, id, "toxin_verifying", "pathogen_retesting"); err != nil {
				return err
			}
		}
		if err := recordReceipt(ctx, tx, id, opID, digest, "toxin"); err != nil {
			return err
		}
		return audit(ctx, tx, id, "evidence", "TOXIN", "toxin evidence versions appended")
	})
}

// toxinClosed reports whether every toxin hole has at least one reading.
func (s *Service) toxinClosed(ctx context.Context, tx *store.Tx, id domain.TaskID) (bool, error) {
	leases, err := tx.ListLeasesByTask(ctx, id)
	if err != nil {
		return false, err
	}
	toxinHoles := map[string]bool{}
	for _, l := range leases {
		if l.Kind == "toxin_hole" {
			toxinHoles[l.ResourceKey] = true
		}
	}
	if len(toxinHoles) == 0 {
		return true, nil
	}
	readings, err := tx.ListToxin(ctx, id)
	if err != nil {
		return false, err
	}
	seen := map[string]bool{}
	for _, r := range readings {
		seen[r.Hole] = true
	}
	for h := range toxinHoles {
		if !seen[h] {
			return false, nil
		}
	}
	return true, nil
}

func sealsOf(cages []store.CageRecord) []string {
	out := make([]string, len(cages))
	for i, c := range cages {
		out[i] = c.Seal
	}
	return out
}

func idsOfTP(tps []store.TimepointRecord) []string {
	out := make([]string, len(tps))
	for i, t := range tps {
		out[i] = t.ID
	}
	return out
}

func idsOfOP(ops []store.ObservationPointRecord) []string {
	out := make([]string, len(ops))
	for i, o := range ops {
		out[i] = o.ID
	}
	return out
}

func parseWater(w WaterInput, sc catalog.Scales) (store.WaterRecord, error) {
	turbidity, err := ledger.ParseScaled(w.Turbidity, sc.TurbidityDecimals, false)
	if err != nil {
		return store.WaterRecord{}, fixedPointErr(err)
	}
	salinity, err := ledger.ParseScaled(w.Salinity, sc.SalinityDecimals, false)
	if err != nil {
		return store.WaterRecord{}, fixedPointErr(err)
	}
	temperature, err := ledger.ParseScaled(w.Temperature, sc.TemperatureDecimals, true)
	if err != nil {
		return store.WaterRecord{}, fixedPointErr(err)
	}
	chlorine, err := ledger.ParseScaled(w.Chlorine, sc.ChlorineDecimals, false)
	if err != nil {
		return store.WaterRecord{}, fixedPointErr(err)
	}
	return store.WaterRecord{
		CageSeal: w.CageSeal, TimepointID: w.TimepointID,
		Turbidity: turbidity, Salinity: salinity, Temperature: temperature, Chlorine: chlorine, Version: 1,
	}, nil
}

func parseToxin(t ToxinInput, sc catalog.Scales) (store.ToxinRecord, error) {
	pspRaw, err := ledger.ParseScaled(t.PSPRaw, sc.PSPDecimals, false)
	if err != nil {
		return store.ToxinRecord{}, fixedPointErr(err)
	}
	dspRaw, err := ledger.ParseScaled(t.DSPRaw, sc.DSPDecimals, false)
	if err != nil {
		return store.ToxinRecord{}, fixedPointErr(err)
	}
	tr := store.ToxinRecord{Hole: t.Hole, PSPRaw: pspRaw, DSPRaw: dspRaw, Version: 1, Valid: true}
	if err := deriveToxin(&tr, t.Factor, t.Divisor); err != nil {
		return store.ToxinRecord{}, err
	}
	return tr, nil
}

func deriveToxin(tr *store.ToxinRecord, factor, divisor int64) error {
	if factor == 0 {
		factor = 1
	}
	if divisor == 0 {
		divisor = 1
	}
	psp, err := ledger.DeriveScaled(tr.PSPRaw, factor, divisor)
	if err != nil {
		return err
	}
	dsp, err := ledger.DeriveScaled(tr.DSPRaw, factor, divisor)
	if err != nil {
		return err
	}
	tr.PSP = psp
	tr.DSP = dsp
	return nil
}

func fixedPointErr(err error) error {
	return &domain.APIError{Code: domain.CodeFixedPointOverflow, Message: "invalid fixed-point value: " + err.Error()}
}
