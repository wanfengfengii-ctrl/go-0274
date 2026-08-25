package service_test

import (
	"context"
	"fmt"
	"testing"

	"oyster-purification-release-gate/adjudication"
	"oyster-purification-release-gate/catalog"
	"oyster-purification-release-gate/domain"
	"oyster-purification-release-gate/service"
	"oyster-purification-release-gate/store"
)

func TestModel_RecoveryRedrivesOnlyEligibleDeviceCalls(t *testing.T) {
	type callCase struct {
		hole            string
		status          string
		attempts        int
		nextRetry       int64
		priorEvidenceCT int64
		wantStatus      string
		wantAttempts    int
		wantEvidenceCT  int64
	}
	tests := []struct {
		name          string
		calls         []callCase
		outcomes      []adjudication.ScriptOutcome
		wantRecovered int
	}{
		{
			name: "persisted pending call without an attempt is driven immediately",
			calls: []callCase{{
				hole: "QPCR-PENDING", status: "pending", attempts: 0,
				wantStatus: "success", wantAttempts: 1, wantEvidenceCT: 180,
			}},
			outcomes:      []adjudication.ScriptOutcome{{Payload: []byte(`{"norovirus_ct":"18.0"}`)}},
			wantRecovered: 1,
		},
		{
			name: "due failed calls retain deterministic insertion order",
			calls: []callCase{
				{hole: "QPCR-FIRST", status: "failed", attempts: 1, nextRetry: 100, wantStatus: "success", wantAttempts: 2, wantEvidenceCT: 110},
				{hole: "QPCR-SECOND", status: "failed", attempts: 1, nextRetry: 99, wantStatus: "success", wantAttempts: 2, wantEvidenceCT: 220},
			},
			outcomes: []adjudication.ScriptOutcome{
				{Payload: []byte(`{"norovirus_ct":"11.0"}`)},
				{Payload: []byte(`{"norovirus_ct":"22.0"}`)},
			},
			wantRecovered: 2,
		},
		{
			name: "successful and not-yet-due calls remain untouched",
			calls: []callCase{
				{hole: "QPCR-DONE", status: "success", attempts: 1, priorEvidenceCT: 330, wantStatus: "success", wantAttempts: 1, wantEvidenceCT: 330},
				{hole: "QPCR-FUTURE", status: "failed", attempts: 1, nextRetry: 101, wantStatus: "failed", wantAttempts: 1},
			},
			wantRecovered: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			st, err := store.Open(ctx, ":memory:", domain.FixedClock(50))
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = st.Close() })

			taskID := domain.TaskID("recovery-task")
			callIDs := make(map[string]int64, len(tc.calls))
			err = st.Tx(ctx, func(tx *store.Tx) error {
				if err := tx.InsertTask(ctx, store.TaskRecord{
					ID: taskID, Status: "pathogen_retesting", Generation: 1,
					AreaID: "east", RuleSnapshotID: catalog.DefaultSnapshotID,
				}); err != nil {
					return err
				}
				for _, c := range tc.calls {
					if err := tx.InsertLease(ctx, store.LeaseRecord{
						Kind: "qpcr_hole", ResourceKey: c.hole, TaskID: taskID,
						Generation: 1, AcquiredAt: 1, ExpiresAt: 1 << 40,
					}); err != nil {
						return err
					}
					id, err := tx.InsertDeviceCall(ctx, store.DeviceCallRecord{
						TaskID: taskID, Kind: "qpcr", Hole: c.hole, Generation: 1,
						Status: c.status, Attempts: c.attempts,
					})
					if err != nil {
						return err
					}
					callIDs[c.hole] = id
					if c.attempts > 0 {
						summary := "retryable failure"
						category := string(adjudication.FailureTimeout)
						if c.status == "success" {
							summary, category = "success", ""
						}
						if err := tx.InsertDeviceAttempt(ctx, store.DeviceAttemptRecord{
							CallID: id, Attempt: c.attempts, Category: category,
							NextRetry: c.nextRetry, Summary: summary,
						}); err != nil {
							return err
						}
					}
					if c.priorEvidenceCT != 0 {
						if err := tx.InsertPathogen(ctx, store.PathogenRecord{
							TaskID: taskID, Generation: 1, Hole: c.hole, Kind: "qpcr",
							NorovirusCt: c.priorEvidenceCT, Version: 1, Valid: true,
						}); err != nil {
							return err
						}
					}
				}
				return nil
			})
			if err != nil {
				t.Fatalf("seed persisted recovery state: %v", err)
			}

			svc := service.New(st, catalog.DefaultCatalog(), adjudication.NewScriptAdapter(tc.outcomes...))
			gotRecovered, err := svc.Recover(ctx, 100)
			if err != nil {
				t.Fatalf("recover: %v", err)
			}
			if gotRecovered != tc.wantRecovered {
				t.Fatalf("recovered %d calls, want %d", gotRecovered, tc.wantRecovered)
			}

			err = st.Tx(ctx, func(tx *store.Tx) error {
				leases, err := tx.ListLeasesByTask(ctx, taskID)
				if err != nil {
					return err
				}
				if len(leases) != len(tc.calls) {
					return fmt.Errorf("recovery changed leases: got %d, want %d", len(leases), len(tc.calls))
				}

				evidence, err := tx.ListPathogen(ctx, taskID)
				if err != nil {
					return err
				}
				byHole := make(map[string][]store.PathogenRecord, len(evidence))
				for _, ev := range evidence {
					byHole[ev.Hole] = append(byHole[ev.Hole], ev)
				}
				for _, c := range tc.calls {
					got, err := tx.GetDeviceCall(ctx, taskID, "qpcr", c.hole, 1)
					if err != nil {
						return err
					}
					if got.ID != callIDs[c.hole] || got.Status != c.wantStatus || got.Attempts != c.wantAttempts {
						return fmt.Errorf("call %s became id=%d status=%s attempts=%d, want id=%d status=%s attempts=%d",
							c.hole, got.ID, got.Status, got.Attempts, callIDs[c.hole], c.wantStatus, c.wantAttempts)
					}
					gotEvidence := byHole[c.hole]
					if c.wantEvidenceCT == 0 {
						if len(gotEvidence) != 0 {
							return fmt.Errorf("call %s unexpectedly gained %d evidence rows", c.hole, len(gotEvidence))
						}
						continue
					}
					if len(gotEvidence) != 1 || gotEvidence[0].NorovirusCt != c.wantEvidenceCT {
						return fmt.Errorf("call %s evidence = %+v, want one row with Ct %d", c.hole, gotEvidence, c.wantEvidenceCT)
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
