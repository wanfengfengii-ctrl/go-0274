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

func mustAdvance(t *testing.T, svc *Service, id domain.TaskID) {
	t.Helper()
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
}

func TestDeviceRetryDeterministic(t *testing.T) {
	clock := domain.NewSteppingClock(0, 1)
	st, err := store.Open(context.Background(), ":memory:", clock)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	adapter := adjudication.NewScriptAdapter(
		adjudication.ScriptOutcome{Failure: adjudication.FailureTimeout},
		adjudication.ScriptOutcome{Failure: adjudication.FailureDisconnected},
		adjudication.ScriptOutcome{Payload: []byte(`{"norovirus_ct":"18.0"}`)},
	)
	svc := New(st, catalog.DefaultCatalog(), adapter)
	id := mustLock(t, svc, "POOL-D")
	mustAdvance(t, svc, id)

	resp, err := svc.StartDeviceCall(context.Background(), id, "op-q", 1, DeviceCallRequest{Hole: "H2", Kind: "qpcr"})
	if err != nil {
		t.Fatalf("device call: %v", err)
	}
	if resp.Status != "failed" {
		t.Fatalf("expected first call to fail, got %s", resp.Status)
	}

	// A retry before the scheduled logical next_retry is gated and must not
	// re-run the instrument: the first failure scheduled next_retry at 1 while
	// the clock is still at 0.
	respPending, err := svc.RetryDeviceCall(context.Background(), resp.CallID)
	if err == nil {
		t.Fatalf("expected early retry to be gated, got status %s", respPending.Status)
	}
	var apiErr *domain.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != domain.CodeDeviceRetryPending {
		t.Fatalf("expected DEVICE_RETRY_PENDING before retry time, got %v", err)
	}
	// No new attempt must have been recorded by the gated retry.
	var attemptsAfterGate int
	_ = st.Tx(context.Background(), func(tx *store.Tx) error {
		row := tx.QueryRowContext(context.Background(), `SELECT attempts FROM device_calls WHERE id = ?`, resp.CallID)
		return row.Scan(&attemptsAfterGate)
	})
	if attemptsAfterGate != 1 {
		t.Fatalf("gated retry must not run the device, expected 1 attempt, got %d", attemptsAfterGate)
	}

	// Advance the clock to the scheduled retry time and retry: disconnected.
	clock.SetTime(1)
	resp, err = svc.RetryDeviceCall(context.Background(), resp.CallID)
	if err != nil {
		t.Fatalf("retry 1: %v", err)
	}
	if resp.Status != "failed" {
		t.Fatalf("expected retry 1 to fail, got %s", resp.Status)
	}
	// Second retry: advance the clock past the new backoff and succeed.
	clock.SetTime(3)
	resp, err = svc.RetryDeviceCall(context.Background(), resp.CallID)
	if err != nil {
		t.Fatalf("retry 2: %v", err)
	}
	if resp.Status != "success" {
		t.Fatalf("expected retry 2 to succeed, got %s", resp.Status)
	}
	// Attempt count must be exactly three (initial + two retries).
	var attempts int
	_ = st.Tx(context.Background(), func(tx *store.Tx) error {
		row := tx.QueryRowContext(context.Background(), `SELECT attempts FROM device_calls WHERE id = ?`, resp.CallID)
		return row.Scan(&attempts)
	})
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestBlindCodeRevealDoesNotLeakMapping(t *testing.T) {
	svc := newTestService(t, ":memory:")
	id := mustLock(t, svc, "POOL-B")
	if err := svc.ConfirmIntake(context.Background(), id, "op-i", 1, IntakeRequest{Confirmers: []string{"C1", "C2"}}); err != nil {
		t.Fatalf("intake: %v", err)
	}
	if err := svc.SealSamples(context.Background(), id, "op-s", 1, SealRequest{Seals: []SealSpec{{CageSeal: "C1", Location: "L1", BlindCodes: []string{"B1", "B2", "B3"}}}}); err != nil {
		t.Fatalf("seal: %v", err)
	}

	// The task view lists codes but never their sample mapping.
	task, err := svc.GetTask(context.Background(), id)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if len(task.BlindCodes) != 3 {
		t.Fatalf("expected 3 blind codes, got %v", task.BlindCodes)
	}

	// Reveal returns the sample mapping only after authorization.
	sampleID, err := svc.RevealBlindCode(context.Background(), "op-reveal", RevealRequest{Code: "B1"})
	if err != nil {
		t.Fatalf("reveal: %v", err)
	}
	if sampleID == "" {
		t.Fatalf("expected a sample id after reveal")
	}

	// A duplicate blind code on another open task is rejected by the unique
	// constraint via the lock path.
	task2, _ := svc.CreateTask(context.Background(), CreateTaskRequest{AreaID: "east", RuleSnapshotID: catalog.DefaultSnapshotID, RuleDigest: snapDigest()})
	_, err = svc.LockTask(context.Background(), domain.TaskID(task2.ID), "op-lock2", 1, LockRequest{
		TideBatch: "T2", UVLampBatch: "UV2", LockedCount: 10,
		CageSeals: []string{"C2"}, Timepoints: []string{"TP1"}, ObservationPoints: []string{"OP1"},
		BlindCodes: []string{"B1", "B4", "B5"}, Pool: "POOL-B2", PumpBranch: "PU2", ProbeWindow: "PR2",
		Holes: []HoleSpec{{ID: "H1", Kind: "toxin"}}, Reviewers: []string{"R1", "R2"},
	})
	if err == nil {
		t.Fatalf("expected duplicate blind code B1 to be rejected")
	}
}

func TestRecheckBumpsGenerationOnce(t *testing.T) {
	svc := newTestService(t, ":memory:")
	id := mustLock(t, svc, "POOL-R2")

	if err := svc.CreateRecheck(context.Background(), id, "op-rk", 1, RecheckRequest{
		Reason: "mortality exceeded", CageSeals: []string{"C1"}, Timepoints: []string{"TP1"}, Holes: []string{"H1"},
	}); err != nil {
		t.Fatalf("recheck: %v", err)
	}
	task, err := svc.GetTask(context.Background(), id)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if task.Generation != 2 {
		t.Fatalf("expected generation bumped to 2, got %d", task.Generation)
	}
	// A second recheck at the now-stale generation must be rejected so only one
	// batch exists per current generation.
	err = svc.CreateRecheck(context.Background(), id, "op-rk2", 1, RecheckRequest{
		Reason: "second", CageSeals: []string{"C1"},
	})
	var apiErr *domain.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != domain.CodeStaleGeneration {
		t.Fatalf("expected STALE_GENERATION for second recheck, got %v", err)
	}
}
