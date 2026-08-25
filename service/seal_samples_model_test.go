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

func TestModel_SealSamplesValidatesBlindCodesAtomically(t *testing.T) {
	tests := []struct {
		name        string
		arrange     string
		blindCodes  []string
		wantSuccess bool
	}{
		{
			name:        "own unbound triplicate succeeds and identical operation retry is idempotent",
			blindCodes:  []string{"TARGET-B1", "TARGET-B2", "TARGET-B3"},
			wantSuccess: true,
		},
		{
			name:       "blind code owned by another open task is rejected",
			arrange:    "foreign",
			blindCodes: []string{"TARGET-B1", "TARGET-B2", "FOREIGN-B1"},
		},
		{
			name:       "blind code already bound in the current task is rejected",
			arrange:    "bound",
			blindCodes: []string{"TARGET-B1", "TARGET-B2", "TARGET-B3"},
		},
		{
			name:       "unknown blind code is rejected",
			arrange:    "unknown",
			blindCodes: []string{"TARGET-B1", "TARGET-B2", "UNKNOWN-B1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			st, err := store.Open(ctx, ":memory:", domain.FixedClock(0))
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = st.Close() })
			svc := New(st, catalog.DefaultCatalog(), adjudication.StaticAdapter{})

			lockTask := func(prefix, tide string, blindCodes []string) domain.TaskID {
				t.Helper()
				created, err := svc.CreateTask(ctx, CreateTaskRequest{
					AreaID:         "east",
					RuleSnapshotID: catalog.DefaultSnapshotID,
					RuleDigest:     catalog.DefaultSnapshot().Digest,
				})
				if err != nil {
					t.Fatalf("create %s task: %v", prefix, err)
				}
				id := domain.TaskID(created.ID)
				_, err = svc.LockTask(ctx, id, "lock-"+prefix, 1, LockRequest{
					TideBatch:         tide,
					UVLampBatch:       prefix + "-UV",
					LockedCount:       10,
					CageSeals:         []string{prefix + "-CAGE"},
					Timepoints:        []string{"TP1"},
					ObservationPoints: []string{"OP1"},
					BlindCodes:        blindCodes,
					Pool:              prefix + "-POOL",
					PumpBranch:        prefix + "-PUMP",
					ProbeWindow:       prefix + "-PROBE",
					Holes:             []HoleSpec{{ID: prefix + "-HOLE", Kind: "toxin"}},
					Reviewers:         []string{"R1", "R2"},
				})
				if err != nil {
					t.Fatalf("lock %s task: %v", prefix, err)
				}
				return id
			}

			targetID := lockTask("TARGET", "T1", []string{"TARGET-B1", "TARGET-B2", "TARGET-B3"})
			if err := svc.ConfirmIntake(ctx, targetID, "target-intake", 1, IntakeRequest{Confirmers: []string{"I1", "I2"}}); err != nil {
				t.Fatalf("confirm target intake: %v", err)
			}

			var foreignID domain.TaskID
			switch tt.arrange {
			case "foreign":
				foreignID = lockTask("FOREIGN", "T2", []string{"FOREIGN-B1", "FOREIGN-B2", "FOREIGN-B3"})
			case "bound":
				err := st.Tx(ctx, func(tx *store.Tx) error {
					_, err := tx.ExecContext(ctx, `UPDATE blind_codes SET sample_id = ? WHERE task_id = ? AND code = ?`, "preexisting-sample", targetID, "TARGET-B3")
					return err
				})
				if err != nil {
					t.Fatalf("arrange bound blind code: %v", err)
				}
			}

			req := SealRequest{Seals: []SealSpec{{
				CageSeal:   "TARGET-CAGE",
				Location:   "TARGET-FREEZER",
				BlindCodes: tt.blindCodes,
			}}}
			err = svc.SealSamples(ctx, targetID, "target-seal", 1, req)
			if tt.wantSuccess {
				if err != nil {
					t.Fatalf("seal valid triplicate: %v", err)
				}
				if err := svc.SealSamples(ctx, targetID, "target-seal", 1, req); err != nil {
					t.Fatalf("retry identical seal operation: %v", err)
				}
			} else {
				var apiErr *domain.APIError
				if !errors.As(err, &apiErr) || apiErr.Code != domain.CodeDuplicateBlindCode {
					t.Fatalf("expected %s, got %v", domain.CodeDuplicateBlindCode, err)
				}
			}

			taskView, err := svc.GetTask(ctx, targetID)
			if err != nil {
				t.Fatalf("get target task: %v", err)
			}

			var samples []store.SampleRecord
			var seals []store.SealRecord
			var targetCodes []store.BlindCodeRecord
			var foreignCodes []store.BlindCodeRecord
			var receipt *store.ReceiptRecord
			err = st.Tx(ctx, func(tx *store.Tx) error {
				var err error
				if samples, err = tx.ListSamplesByTask(ctx, targetID); err != nil {
					return err
				}
				if seals, err = tx.ListSealsByTask(ctx, targetID); err != nil {
					return err
				}
				if targetCodes, err = tx.ListBlindCodesByTask(ctx, targetID); err != nil {
					return err
				}
				if foreignID != "" {
					if foreignCodes, err = tx.ListBlindCodesByTask(ctx, foreignID); err != nil {
						return err
					}
				}
				receipt, err = tx.GetReceipt(ctx, "target-seal")
				return err
			})

			if tt.wantSuccess {
				if err != nil {
					t.Fatalf("inspect successful transaction: %v", err)
				}
				if taskView.Status != "resources_busy" || len(samples) != 3 || len(seals) != 1 || receipt == nil {
					t.Fatalf("successful retry changed outcome: status=%s samples=%d seals=%d receipt=%v", taskView.Status, len(samples), len(seals), receipt != nil)
				}
				for _, code := range targetCodes {
					if code.SampleID == "" {
						t.Fatalf("blind code %s was not bound", code.Code)
					}
				}
				return
			}

			if !store.IsNotFound(err) {
				t.Fatalf("failed seal left a receipt or inspection failed: %v", err)
			}
			if taskView.Status != "sealing_samples" || len(samples) != 0 || len(seals) != 0 {
				t.Fatalf("failed seal was not atomic: status=%s samples=%d seals=%d", taskView.Status, len(samples), len(seals))
			}
			for _, code := range targetCodes {
				wantSampleID := ""
				if tt.arrange == "bound" && code.Code == "TARGET-B3" {
					wantSampleID = "preexisting-sample"
				}
				if code.SampleID != wantSampleID {
					t.Fatalf("target blind code %s mapping changed: got %q, want %q", code.Code, code.SampleID, wantSampleID)
				}
			}
			for _, code := range foreignCodes {
				if code.SampleID != "" {
					t.Fatalf("foreign blind code %s was bound to target sample %q", code.Code, code.SampleID)
				}
			}
		})
	}
}
