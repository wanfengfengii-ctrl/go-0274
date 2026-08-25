package service

import (
	"context"

	"oyster-purification-release-gate/catalog"
	"oyster-purification-release-gate/domain"
	"oyster-purification-release-gate/ledger"
	"oyster-purification-release-gate/store"
)

// permilleScale is the integer factor used to express mortality as permille
// (parts per thousand), keeping every threshold comparison integer-only.
const permilleScale = 1000

// evaluateMortality audits any vitality cell whose dead-plus-broken mortality,
// expressed in permille, breaches the snapshot threshold. It never rejects the
// submission: the anomaly is recorded for a later recheck, exactly as the
// project flow requires.
func (s *Service) evaluateMortality(ctx context.Context, tx *store.Tx, rec *store.TaskRecord, cells []VitalityCellInput) error {
	snap, ok := s.snapshotFor(rec)
	if !ok {
		return nil
	}
	scale, err := ledger.Scale(3) // 10^3 == permilleScale
	if err != nil {
		return err
	}
	_ = scale
	for _, c := range cells {
		deadBroken := c.Dead + c.Broken
		permille := deadBroken * permilleScale / rec.LockedCount
		if ledger.CompareScaled(permille, snap.Thresholds.MaxMortalityPermille) > 0 {
			if err := audit(ctx, tx, rec.ID, "anomaly", "MORTALITY_EXCEEDED",
				"cage "+c.CageSeal+" mortality exceeded threshold"); err != nil {
				return err
			}
		}
	}
	return nil
}

// evaluateToxin audits any toxin reading whose derived PSP or DSP concentration
// breaches the snapshot threshold. The comparison is integer-only and uses the
// derived scaled values produced by the plate-reader calibration.
func (s *Service) evaluateToxin(ctx context.Context, tx *store.Tx, id domain.TaskID, snap catalog.RuleSnapshot, tr store.ToxinRecord) error {
	if ledger.CompareScaled(tr.PSP, snap.Thresholds.MaxPSP) > 0 {
		return audit(ctx, tx, id, "anomaly", "TOXIN_POSITIVE", "hole "+tr.Hole+" PSP exceeded threshold")
	}
	if ledger.CompareScaled(tr.DSP, snap.Thresholds.MaxDSP) > 0 {
		return audit(ctx, tx, id, "anomaly", "TOXIN_POSITIVE", "hole "+tr.Hole+" DSP exceeded threshold")
	}
	return nil
}
