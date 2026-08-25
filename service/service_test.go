package service

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"oyster-purification-release-gate/adjudication"
	"oyster-purification-release-gate/catalog"
	"oyster-purification-release-gate/domain"
	"oyster-purification-release-gate/store"
)

func newTestService(t *testing.T, dsn string) *Service {
	t.Helper()
	st, err := store.Open(context.Background(), dsn, domain.FixedClock(0))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return New(st, catalog.DefaultCatalog(), adjudication.StaticAdapter{Payload: []byte(`{"norovirus_ct":"18.0","coliform":"2"}`)})
}

func snapDigest() string { return catalog.DefaultSnapshot().Digest }

func mustLock(t *testing.T, svc *Service, pool string) domain.TaskID {
	t.Helper()
	task, err := svc.CreateTask(context.Background(), CreateTaskRequest{
		AreaID: "east", RuleSnapshotID: catalog.DefaultSnapshotID, RuleDigest: snapDigest(),
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	id := domain.TaskID(task.ID)
	_, err = svc.LockTask(context.Background(), id, "op-lock-"+string(id), 1, LockRequest{
		TideBatch: "T1", UVLampBatch: "UV1", LockedCount: 10,
		CageSeals: []string{"C1"}, Timepoints: []string{"TP1"}, ObservationPoints: []string{"OP1"},
		BlindCodes: []string{"B1", "B2", "B3"}, Pool: pool, PumpBranch: "PU1", ProbeWindow: "PR1",
		Holes:     []HoleSpec{{ID: "H1", Kind: "toxin"}, {ID: "H2", Kind: "qpcr"}, {ID: "H3", Kind: "culture"}},
		Reviewers: []string{"R1", "R2"},
	})
	if err != nil {
		t.Fatalf("lock task: %v", err)
	}
	return id
}

func TestFullHappyPathRelease(t *testing.T) {
	svc := newTestService(t, ":memory:")
	id := mustLock(t, svc, "POOL-A")

	if err := svc.ConfirmIntake(context.Background(), id, "op-intake", 1, IntakeRequest{Confirmers: []string{"C1", "C2"}}); err != nil {
		t.Fatalf("intake: %v", err)
	}
	if err := svc.SealSamples(context.Background(), id, "op-seal", 1, SealRequest{Seals: []SealSpec{
		{CageSeal: "C1", Location: "L1", BlindCodes: []string{"B1", "B2", "B3"}},
	}}); err != nil {
		t.Fatalf("seal: %v", err)
	}
	if err := svc.SubmitWater(context.Background(), id, "op-water", 1, WaterRequest{Readings: []WaterInput{
		{CageSeal: "C1", TimepointID: "TP1", Turbidity: "3.0", Salinity: "30.00", Temperature: "22.00", Chlorine: "0.20"},
	}}); err != nil {
		t.Fatalf("water: %v", err)
	}
	if err := svc.SubmitVitality(context.Background(), id, "op-vit", 1, VitalityRequest{Cells: []VitalityCellInput{
		{CageSeal: "C1", TimepointID: "TP1", ObservationPointID: "OP1", Closed: 9, WeakOpen: 1, Dead: 0, Broken: 0},
	}}); err != nil {
		t.Fatalf("vitality: %v", err)
	}
	if err := svc.SubmitToxin(context.Background(), id, "op-toxin", 1, ToxinRequest{Readings: []ToxinInput{
		{Hole: "H1", PSPRaw: "0.500", DSPRaw: "0.100", Factor: 1, Divisor: 1},
	}}); err != nil {
		t.Fatalf("toxin: %v", err)
	}
	if _, err := svc.StartDeviceCall(context.Background(), id, "op-qpcr", 1, DeviceCallRequest{Hole: "H2", Kind: "qpcr"}); err != nil {
		t.Fatalf("qpcr: %v", err)
	}
	if _, err := svc.StartDeviceCall(context.Background(), id, "op-culture", 1, DeviceCallRequest{Hole: "H3", Kind: "incubator"}); err != nil {
		t.Fatalf("incubator: %v", err)
	}
	if err := svc.SubmitReview(context.Background(), id, "op-r1", 1, ReviewRequest{Reviewer: "R1", Approved: true}); err != nil {
		t.Fatalf("review R1: %v", err)
	}
	if err := svc.SubmitReview(context.Background(), id, "op-r2", 1, ReviewRequest{Reviewer: "R2", Approved: true}); err != nil {
		t.Fatalf("review R2: %v", err)
	}

	task, err := svc.GetTask(context.Background(), id)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if task.Status != "releasable" {
		t.Fatalf("expected releasable, got %s", task.Status)
	}

	if _, err := svc.Decide(context.Background(), id, "op-release", 1, "release", DecisionRequest{Reason: "all clear"}); err != nil {
		t.Fatalf("release: %v", err)
	}
	permit, err := svc.GetPermit(context.Background(), id)
	if err != nil {
		t.Fatalf("permit: %v", err)
	}
	if permit.PermitNumber == "" {
		t.Fatalf("expected a permit number")
	}
	task, _ = svc.GetTask(context.Background(), id)
	if task.Status != "released" {
		t.Fatalf("expected released, got %s", task.Status)
	}
	// Leases must be released after the terminal decision.
	var leaseCount int
	_ = svc.store.Tx(context.Background(), func(tx *store.Tx) error {
		leases, err := tx.ListLeasesByTask(context.Background(), id)
		leaseCount = len(leases)
		return err
	})
	if leaseCount != 0 {
		t.Fatalf("expected leases released, got %d", leaseCount)
	}
}

func TestLockRejectsStaleDigest(t *testing.T) {
	svc := newTestService(t, ":memory:")
	task, err := svc.CreateTask(context.Background(), CreateTaskRequest{
		AreaID: "east", RuleSnapshotID: catalog.DefaultSnapshotID, RuleDigest: "sha256:wrong",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = svc.LockTask(context.Background(), domain.TaskID(task.ID), "op", 1, LockRequest{
		TideBatch: "T1", UVLampBatch: "UV1", LockedCount: 10,
		CageSeals: []string{"C1"}, Timepoints: []string{"TP1"}, ObservationPoints: []string{"OP1"},
		BlindCodes: []string{"B1", "B2", "B3"}, Pool: "P", PumpBranch: "PU", ProbeWindow: "PR",
		Holes: []HoleSpec{{ID: "H1", Kind: "toxin"}}, Reviewers: []string{"R1", "R2"},
	})
	var apiErr *domain.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != domain.CodeStaleRuleDigest {
		t.Fatalf("expected STALE_RULE_DIGEST, got %v", err)
	}
}

func TestLockRejectsAreaTideMismatch(t *testing.T) {
	svc := newTestService(t, ":memory:")
	task, err := svc.CreateTask(context.Background(), CreateTaskRequest{
		AreaID: "east", RuleSnapshotID: catalog.DefaultSnapshotID, RuleDigest: snapDigest(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = svc.LockTask(context.Background(), domain.TaskID(task.ID), "op", 1, LockRequest{
		TideBatch: "T9", UVLampBatch: "UV1", LockedCount: 10,
		CageSeals: []string{"C1"}, Timepoints: []string{"TP1"}, ObservationPoints: []string{"OP1"},
		BlindCodes: []string{"B1", "B2", "B3"}, Pool: "P", PumpBranch: "PU", ProbeWindow: "PR",
		Holes: []HoleSpec{{ID: "H1", Kind: "toxin"}}, Reviewers: []string{"R1", "R2"},
	})
	var apiErr *domain.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != domain.CodeAreaTideMismatch {
		t.Fatalf("expected AREA_TIDE_MISMATCH, got %v", err)
	}
}

func TestLeaseConcurrencySingleWinner(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "lease.db")
	svc := newTestService(t, dsn)

	const pool = "SHARED-POOL"
	var wg sync.WaitGroup
	results := make(chan error, 2)
	start := make(chan struct{})

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			suffix := string(rune('a' + i))
			task, err := svc.CreateTask(context.Background(), CreateTaskRequest{
				AreaID: "east", RuleSnapshotID: catalog.DefaultSnapshotID, RuleDigest: snapDigest(),
			})
			if err != nil {
				results <- err
				return
			}
			_, err = svc.LockTask(context.Background(), domain.TaskID(task.ID), "op-"+string(task.ID), 1, LockRequest{
				TideBatch: "T1", UVLampBatch: "UV1", LockedCount: 10,
				CageSeals: []string{"C1-" + suffix}, Timepoints: []string{"TP1"}, ObservationPoints: []string{"OP1"},
				BlindCodes: []string{"B1-" + suffix, "B2-" + suffix, "B3-" + suffix},
				Pool:       pool, PumpBranch: "PU1", ProbeWindow: "PR1",
				Holes: []HoleSpec{{ID: "H1-" + suffix, Kind: "toxin"}}, Reviewers: []string{"R1", "R2"},
			})
			results <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)

	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		var apiErr *domain.APIError
		if errors.As(err, &apiErr) && apiErr.Code == domain.CodeResourceBusy {
			conflicts++
		} else {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("expected exactly one winner, got %d successes %d conflicts", successes, conflicts)
	}
}

func TestIdempotencyReplayAndConflict(t *testing.T) {
	svc := newTestService(t, ":memory:")
	id := mustLock(t, svc, "POOL-X")

	req := IntakeRequest{Confirmers: []string{"C1", "C2"}}
	if err := svc.ConfirmIntake(context.Background(), id, "op-intake", 1, req); err != nil {
		t.Fatalf("intake: %v", err)
	}
	// Same op id and content replays without error.
	if err := svc.ConfirmIntake(context.Background(), id, "op-intake", 1, req); err != nil {
		t.Fatalf("replay should succeed: %v", err)
	}
	// Same op id with different content conflicts.
	req2 := IntakeRequest{Confirmers: []string{"C3", "C4"}}
	err := svc.ConfirmIntake(context.Background(), id, "op-intake", 1, req2)
	var apiErr *domain.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != domain.CodeOperationConflict {
		t.Fatalf("expected OPERATION_CONFLICT, got %v", err)
	}
}

func TestTerminalRaceSingleWinner(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "race.db")
	svc := newTestService(t, dsn)
	id := mustLock(t, svc, "POOL-R")

	// Drive the task to releasable quickly.
	if err := svc.ConfirmIntake(context.Background(), id, "op-i", 1, IntakeRequest{Confirmers: []string{"C1", "C2"}}); err != nil {
		t.Fatalf("intake: %v", err)
	}
	if err := svc.SealSamples(context.Background(), id, "op-s", 1, SealRequest{Seals: []SealSpec{{CageSeal: "C1", Location: "L1", BlindCodes: []string{"B1", "B2", "B3"}}}}); err != nil {
		t.Fatalf("seal: %v", err)
	}
	if err := svc.SubmitVitality(context.Background(), id, "op-v", 1, VitalityRequest{Cells: []VitalityCellInput{{CageSeal: "C1", TimepointID: "TP1", ObservationPointID: "OP1", Closed: 10}}}); err != nil {
		t.Fatalf("vitality: %v", err)
	}
	if err := svc.SubmitToxin(context.Background(), id, "op-t", 1, ToxinRequest{Readings: []ToxinInput{{Hole: "H1", PSPRaw: "0.1", DSPRaw: "0.1", Factor: 1, Divisor: 1}}}); err != nil {
		t.Fatalf("toxin: %v", err)
	}
	if _, err := svc.StartDeviceCall(context.Background(), id, "op-q", 1, DeviceCallRequest{Hole: "H2", Kind: "qpcr"}); err != nil {
		t.Fatalf("qpcr: %v", err)
	}
	if _, err := svc.StartDeviceCall(context.Background(), id, "op-c", 1, DeviceCallRequest{Hole: "H3", Kind: "incubator"}); err != nil {
		t.Fatalf("incubator: %v", err)
	}
	if err := svc.SubmitReview(context.Background(), id, "op-r1", 1, ReviewRequest{Reviewer: "R1", Approved: true}); err != nil {
		t.Fatalf("r1: %v", err)
	}
	if err := svc.SubmitReview(context.Background(), id, "op-r2", 1, ReviewRequest{Reviewer: "R2", Approved: true}); err != nil {
		t.Fatalf("r2: %v", err)
	}

	decisions := []string{"release", "isolate", "cancel"}
	var wg sync.WaitGroup
	results := make(chan error, len(decisions))
	start := make(chan struct{})
	for _, d := range decisions {
		wg.Add(1)
		go func(d string) {
			defer wg.Done()
			<-start
			_, err := svc.Decide(context.Background(), id, "op-"+d, 1, d, DecisionRequest{Reason: "race"})
			results <- err
		}(d)
	}
	close(start)
	wg.Wait()
	close(results)

	winners := 0
	for err := range results {
		if err == nil {
			winners++
			continue
		}
		var apiErr *domain.APIError
		if !errors.As(err, &apiErr) || apiErr.Code != domain.CodeTerminalState {
			t.Fatalf("expected TERMINAL_STATE for losers, got %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("expected exactly one terminal winner, got %d", winners)
	}
}
