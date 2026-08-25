package service

import (
	"context"
	"errors"
	"testing"

	"oyster-purification-release-gate/adjudication"
	"oyster-purification-release-gate/catalog"
	"oyster-purification-release-gate/domain"
	"oyster-purification-release-gate/store"
)

func TestModel_ToxinCalibrationOverflowRejectsBatchAtomically(t *testing.T) {
	cases := []struct {
		name   string
		pspRaw string
		dspRaw string
	}{
		{name: "PSP multiplication overflows", pspRaw: "9223372036854775.807", dspRaw: "0.001"},
		{name: "DSP multiplication overflows", pspRaw: "0.001", dspRaw: "9223372036854775.807"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			st, err := store.Open(ctx, ":memory:", domain.FixedClock(0))
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = st.Close() })
			svc := New(st, catalog.DefaultCatalog(), adjudication.StaticAdapter{})

			created, err := svc.CreateTask(ctx, CreateTaskRequest{
				AreaID: "east", RuleSnapshotID: catalog.DefaultSnapshotID, RuleDigest: catalog.DefaultSnapshot().Digest,
			})
			if err != nil {
				t.Fatalf("create task: %v", err)
			}
			id := domain.TaskID(created.ID)
			_, err = svc.LockTask(ctx, id, "lock", 1, LockRequest{
				TideBatch: "T1", UVLampBatch: "UV1", LockedCount: 10,
				CageSeals: []string{"C1"}, Timepoints: []string{"TP1"}, ObservationPoints: []string{"OP1"},
				BlindCodes: []string{"B1", "B2", "B3"}, Pool: "POOL", PumpBranch: "PUMP", ProbeWindow: "PROBE",
				Holes: []HoleSpec{{ID: "H1", Kind: "toxin"}, {ID: "H2", Kind: "toxin"}}, Reviewers: []string{"R1", "R2"},
			})
			if err != nil {
				t.Fatalf("lock task: %v", err)
			}
			if err := svc.ConfirmIntake(ctx, id, "intake", 1, IntakeRequest{Confirmers: []string{"I1", "I2"}}); err != nil {
				t.Fatalf("confirm intake: %v", err)
			}
			if err := svc.SealSamples(ctx, id, "seal", 1, SealRequest{Seals: []SealSpec{{
				CageSeal: "C1", Location: "L1", BlindCodes: []string{"B1", "B2", "B3"},
			}}}); err != nil {
				t.Fatalf("seal samples: %v", err)
			}
			if err := svc.SubmitVitality(ctx, id, "vitality", 1, VitalityRequest{Cells: []VitalityCellInput{{
				CageSeal: "C1", TimepointID: "TP1", ObservationPointID: "OP1", Closed: 10,
			}}}); err != nil {
				t.Fatalf("submit vitality: %v", err)
			}

			auditBefore, err := svc.GetAudit(ctx, id)
			if err != nil {
				t.Fatalf("get audit before toxin submission: %v", err)
			}
			err = svc.SubmitToxin(ctx, id, "toxin-operation", 1, ToxinRequest{Readings: []ToxinInput{
				{Hole: "H1", PSPRaw: "0.801", DSPRaw: "0.001", Factor: 1, Divisor: 1},
				{Hole: "H2", PSPRaw: tc.pspRaw, DSPRaw: tc.dspRaw, Factor: 2, Divisor: 1},
			}})
			var apiErr *domain.APIError
			if !errors.As(err, &apiErr) || apiErr.Code != domain.CodeFixedPointOverflow {
				t.Fatalf("expected FIXED_POINT_OVERFLOW, got %v", err)
			}

			evidence, err := svc.GetEvidence(ctx, id)
			if err != nil {
				t.Fatalf("get evidence after rejected batch: %v", err)
			}
			if evidence.Toxin != 0 {
				t.Fatalf("rejected batch persisted %d toxin evidence versions", evidence.Toxin)
			}
			task, err := svc.GetTask(ctx, id)
			if err != nil {
				t.Fatalf("get task after rejected batch: %v", err)
			}
			if task.Status != "toxin_verifying" {
				t.Fatalf("rejected batch advanced task to %q", task.Status)
			}
			auditAfter, err := svc.GetAudit(ctx, id)
			if err != nil {
				t.Fatalf("get audit after rejected batch: %v", err)
			}
			if len(auditAfter) != len(auditBefore) {
				t.Fatalf("rejected batch persisted audit events: before=%d after=%d", len(auditBefore), len(auditAfter))
			}

			// Reusing the operation ID with corrected content proves that the failed
			// transaction did not leave a success receipt. The second reading also
			// exercises truncation toward zero at both toxin thresholds.
			err = svc.SubmitToxin(ctx, id, "toxin-operation", 1, ToxinRequest{Readings: []ToxinInput{
				{Hole: "H1", PSPRaw: "0.801", DSPRaw: "0.001", Factor: 1, Divisor: 1},
				{Hole: "H2", PSPRaw: "1.601", DSPRaw: "0.321", Factor: 1, Divisor: 2},
			}})
			if err != nil {
				t.Fatalf("corrected retry with same operation ID: %v", err)
			}
			evidence, err = svc.GetEvidence(ctx, id)
			if err != nil {
				t.Fatalf("get evidence after corrected retry: %v", err)
			}
			if evidence.Toxin != 2 {
				t.Fatalf("corrected batch stored %d toxin evidence versions, want 2", evidence.Toxin)
			}
			task, err = svc.GetTask(ctx, id)
			if err != nil {
				t.Fatalf("get task after corrected retry: %v", err)
			}
			if task.Status != "pathogen_retesting" {
				t.Fatalf("completed corrected batch left task in %q", task.Status)
			}
			auditAfter, err = svc.GetAudit(ctx, id)
			if err != nil {
				t.Fatalf("get audit after corrected retry: %v", err)
			}
			positive := 0
			for _, event := range auditAfter {
				if event.Code == "TOXIN_POSITIVE" {
					positive++
				}
			}
			if positive != 1 {
				t.Fatalf("got %d toxin threshold audit events, want 1", positive)
			}
		})
	}
}
