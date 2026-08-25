package service_test

import (
	"context"
	"errors"
	"testing"

	"oyster-purification-release-gate/adjudication"
	"oyster-purification-release-gate/catalog"
	"oyster-purification-release-gate/domain"
	"oyster-purification-release-gate/service"
	"oyster-purification-release-gate/store"
)

func TestModel_OpenTaskBlindCodeUniqueness(t *testing.T) {
	tests := []struct {
		name   string
		reveal bool
	}{
		{name: "unrevealed code remains reserved", reveal: false},
		{name: "revealed code remains reserved until terminal", reveal: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			st, err := store.Open(ctx, ":memory:", domain.FixedClock(0))
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = st.Close() })
			svc := service.New(st, catalog.DefaultCatalog(), adjudication.StaticAdapter{})
			digest := catalog.DefaultSnapshot().Digest

			create := func() domain.TaskID {
				t.Helper()
				got, err := svc.CreateTask(ctx, service.CreateTaskRequest{
					AreaID: "east", RuleSnapshotID: catalog.DefaultSnapshotID, RuleDigest: digest,
				})
				if err != nil {
					t.Fatalf("create task: %v", err)
				}
				return domain.TaskID(got.ID)
			}
			lock := func(id domain.TaskID, op, tide, cage, code1, code2, code3, suffix string) (*service.TaskResponse, error) {
				t.Helper()
				return svc.LockTask(ctx, id, op, 1, service.LockRequest{
					TideBatch: tide, UVLampBatch: "UV-" + suffix, LockedCount: 10,
					CageSeals: []string{cage}, Timepoints: []string{"TP1"}, ObservationPoints: []string{"OP1"},
					BlindCodes: []string{code1, code2, code3}, Pool: "POOL-" + suffix,
					PumpBranch: "PUMP-" + suffix, ProbeWindow: "PROBE-" + suffix,
					Holes:     []service.HoleSpec{{ID: "HOLE-" + suffix, Kind: "toxin"}},
					Reviewers: []string{"R1", "R2"},
				})
			}

			sourceID := create()
			if _, err := lock(sourceID, "lock-source", "T1", "CAGE-SOURCE", "B1", "B2", "B3", "SOURCE"); err != nil {
				t.Fatalf("lock source task: %v", err)
			}
			if tt.reveal {
				if err := svc.ConfirmIntake(ctx, sourceID, "intake-source", 1, service.IntakeRequest{Confirmers: []string{"C1", "C2"}}); err != nil {
					t.Fatalf("confirm source intake: %v", err)
				}
				if err := svc.SealSamples(ctx, sourceID, "seal-source", 1, service.SealRequest{Seals: []service.SealSpec{{
					CageSeal: "CAGE-SOURCE", Location: "ARCHIVE-1", BlindCodes: []string{"B1", "B2", "B3"},
				}}}); err != nil {
					t.Fatalf("seal source samples: %v", err)
				}
				if sampleID, err := svc.RevealBlindCode(ctx, "reveal-source", service.RevealRequest{Code: "B1"}); err != nil || sampleID == "" {
					t.Fatalf("reveal B1: sample=%q err=%v", sampleID, err)
				}
			}

			targetID := create()
			for attempt := 1; attempt <= 2; attempt++ {
				_, err := lock(targetID, "lock-target", "T2", "CAGE-TARGET", "FRESH-1", "B1", "FRESH-2", "TARGET")
				var apiErr *domain.APIError
				if !errors.As(err, &apiErr) || apiErr.Code != domain.CodeDuplicateBlindCode {
					t.Fatalf("attempt %d: expected %s, got %v", attempt, domain.CodeDuplicateBlindCode, err)
				}

				got, getErr := svc.GetTask(ctx, targetID)
				if getErr != nil {
					t.Fatalf("attempt %d: get rejected target: %v", attempt, getErr)
				}
				if got.Status != "pending_lock" || got.TideBatch != "" || len(got.CageSeals) != 0 || len(got.BlindCodes) != 0 || len(got.Leases) != 0 {
					t.Fatalf("attempt %d: rejected lock left partial state: %+v", attempt, got)
				}
			}

			if _, err := lock(targetID, "lock-target-retry", "T2", "CAGE-TARGET", "FRESH-1", "FRESH-3", "FRESH-2", "TARGET"); err != nil {
				t.Fatalf("lock after replacing duplicate code; rejected transaction was not fully rolled back: %v", err)
			}
		})
	}
}
