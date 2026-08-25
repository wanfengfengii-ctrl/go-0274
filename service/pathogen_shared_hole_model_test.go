package service

import (
	"context"
	"testing"

	"oyster-purification-release-gate/catalog"
	"oyster-purification-release-gate/domain"
)

func TestModel_PathogenClosureRequiresEachDeviceKindForSharedHole(t *testing.T) {
	tests := []struct {
		name   string
		first  string
		second string
	}{
		{name: "qPCR evidence does not cover culture", first: "qpcr", second: "incubator"},
		{name: "culture evidence does not cover qPCR", first: "incubator", second: "qpcr"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			svc := newTestService(t, ":memory:")
			created, err := svc.CreateTask(ctx, CreateTaskRequest{
				AreaID:         "east",
				RuleSnapshotID: catalog.DefaultSnapshotID,
				RuleDigest:     snapDigest(),
			})
			if err != nil {
				t.Fatalf("create task: %v", err)
			}
			id := domain.TaskID(created.ID)

			_, err = svc.LockTask(ctx, id, "op-lock", 1, LockRequest{
				TideBatch: "T1", UVLampBatch: "UV1", LockedCount: 10,
				CageSeals: []string{"C1"}, Timepoints: []string{"TP1"}, ObservationPoints: []string{"OP1"},
				BlindCodes: []string{"B1", "B2", "B3"}, Pool: "POOL", PumpBranch: "PU1", ProbeWindow: "PR1",
				Holes: []HoleSpec{
					{ID: "TOXIN", Kind: "toxin"},
					{ID: "SHARED", Kind: "qpcr"},
					{ID: "SHARED", Kind: "culture"},
				},
				Reviewers: []string{"R1", "R2"},
			})
			if err != nil {
				t.Fatalf("lock task: %v", err)
			}
			if err := svc.ConfirmIntake(ctx, id, "op-intake", 1, IntakeRequest{Confirmers: []string{"C1", "C2"}}); err != nil {
				t.Fatalf("confirm intake: %v", err)
			}
			if err := svc.SealSamples(ctx, id, "op-seal", 1, SealRequest{Seals: []SealSpec{{
				CageSeal: "C1", Location: "L1", BlindCodes: []string{"B1", "B2", "B3"},
			}}}); err != nil {
				t.Fatalf("seal samples: %v", err)
			}
			if err := svc.SubmitVitality(ctx, id, "op-vitality", 1, VitalityRequest{Cells: []VitalityCellInput{{
				CageSeal: "C1", TimepointID: "TP1", ObservationPointID: "OP1", Closed: 10,
			}}}); err != nil {
				t.Fatalf("submit vitality: %v", err)
			}
			if err := svc.SubmitToxin(ctx, id, "op-toxin", 1, ToxinRequest{Readings: []ToxinInput{{
				Hole: "TOXIN", PSPRaw: "0.1", DSPRaw: "0.1", Factor: 1, Divisor: 1,
			}}}); err != nil {
				t.Fatalf("submit toxin: %v", err)
			}

			if _, err := svc.StartDeviceCall(ctx, id, "op-first", 1, DeviceCallRequest{Hole: "SHARED", Kind: tt.first}); err != nil {
				t.Fatalf("submit first pathogen result (%s): %v", tt.first, err)
			}
			task, err := svc.GetTask(ctx, id)
			if err != nil {
				t.Fatalf("get task after first result: %v", err)
			}
			if task.Status != "pathogen_retesting" {
				t.Fatalf("one %s result for a shared hole advanced task to %q; want pathogen_retesting", tt.first, task.Status)
			}
			evidence, err := svc.GetEvidence(ctx, id)
			if err != nil {
				t.Fatalf("get evidence after first result: %v", err)
			}
			if evidence.Pathogen != 1 {
				t.Fatalf("pathogen versions after first result = %d; want 1", evidence.Pathogen)
			}

			if _, err := svc.StartDeviceCall(ctx, id, "op-second", 1, DeviceCallRequest{Hole: "SHARED", Kind: tt.second}); err != nil {
				t.Fatalf("submit second pathogen result (%s): %v", tt.second, err)
			}
			task, err = svc.GetTask(ctx, id)
			if err != nil {
				t.Fatalf("get task after both results: %v", err)
			}
			if task.Status != "pending_review" {
				t.Fatalf("task status after both device kinds = %q; want pending_review", task.Status)
			}
			evidence, err = svc.GetEvidence(ctx, id)
			if err != nil {
				t.Fatalf("get evidence after both results: %v", err)
			}
			if evidence.Pathogen != 2 {
				t.Fatalf("pathogen versions after both results = %d; want two immutable device-specific versions", evidence.Pathogen)
			}
		})
	}
}
