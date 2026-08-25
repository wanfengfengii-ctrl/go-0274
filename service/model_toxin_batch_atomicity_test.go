package service

import (
	"context"
	"testing"

	"oyster-purification-release-gate/domain"
)

func TestModel_ToxinBatchAtomicity(t *testing.T) {
	ctx := context.Background()
	validBatch := ToxinRequest{Readings: []ToxinInput{
		{Hole: "H1", PSPRaw: "0.100", DSPRaw: "0.010", Factor: 1, Divisor: 1},
		{Hole: "H2", PSPRaw: "0.200", DSPRaw: "0.020", Factor: 1, Divisor: 1},
	}}
	tests := []struct {
		name       string
		prepareSQL string
		failed     ToxinRequest
	}{
		{
			name: "second reading cannot be parsed",
			failed: ToxinRequest{Readings: []ToxinInput{
				{Hole: "H1", PSPRaw: "0.100", DSPRaw: "0.010", Factor: 1, Divisor: 1},
				{Hole: "H2", PSPRaw: "not-a-number", DSPRaw: "0.020", Factor: 1, Divisor: 1},
			}},
		},
		{
			name: "second reading calibration overflows",
			failed: ToxinRequest{Readings: []ToxinInput{
				{Hole: "H1", PSPRaw: "0.100", DSPRaw: "0.010", Factor: 1, Divisor: 1},
				{Hole: "H2", PSPRaw: "9000000000000000.000", DSPRaw: "0.020", Factor: 2, Divisor: 1},
			}},
		},
		{
			name:       "second reading persistence fails",
			prepareSQL: `CREATE TRIGGER reject_h2_toxin BEFORE INSERT ON toxin_evidence_versions WHEN NEW.hole = 'H2' BEGIN SELECT RAISE(ABORT, 'injected toxin persistence failure'); END`,
			failed:     validBatch,
		},
		{
			name:       "second reading threshold audit fails",
			prepareSQL: `CREATE TRIGGER reject_toxin_audit BEFORE INSERT ON audit_events WHEN NEW.code = 'TOXIN_POSITIVE' BEGIN SELECT RAISE(ABORT, 'injected toxin audit failure'); END`,
			failed: ToxinRequest{Readings: []ToxinInput{
				{Hole: "H1", PSPRaw: "0.100", DSPRaw: "0.010", Factor: 1, Divisor: 1},
				{Hole: "H2", PSPRaw: "0.900", DSPRaw: "0.020", Factor: 1, Divisor: 1},
			}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestService(t, ":memory:")
			task, err := svc.CreateTask(ctx, CreateTaskRequest{
				AreaID: "east", RuleSnapshotID: "rules-east-1", RuleDigest: snapDigest(),
			})
			if err != nil {
				t.Fatalf("create task: %v", err)
			}
			id := domain.TaskID(task.ID)
			if _, err := svc.LockTask(ctx, id, "lock-"+tc.name, 1, LockRequest{
				TideBatch: "T1", UVLampBatch: "UV1", LockedCount: 10,
				CageSeals: []string{"C1"}, Timepoints: []string{"TP1"}, ObservationPoints: []string{"OP1"},
				BlindCodes: []string{"B1", "B2", "B3"}, Pool: "pool-" + tc.name, PumpBranch: "PU1", ProbeWindow: "PR1",
				Holes: []HoleSpec{{ID: "H1", Kind: "toxin"}, {ID: "H2", Kind: "toxin"}}, Reviewers: []string{"R1", "R2"},
			}); err != nil {
				t.Fatalf("lock task: %v", err)
			}
			if err := svc.ConfirmIntake(ctx, id, "intake-"+tc.name, 1, IntakeRequest{Confirmers: []string{"C1", "C2"}}); err != nil {
				t.Fatalf("confirm intake: %v", err)
			}
			if err := svc.SealSamples(ctx, id, "seal-"+tc.name, 1, SealRequest{Seals: []SealSpec{{CageSeal: "C1", Location: "L1", BlindCodes: []string{"B1", "B2", "B3"}}}}); err != nil {
				t.Fatalf("seal samples: %v", err)
			}
			if err := svc.SubmitVitality(ctx, id, "vitality-"+tc.name, 1, VitalityRequest{Cells: []VitalityCellInput{{CageSeal: "C1", TimepointID: "TP1", ObservationPointID: "OP1", Closed: 10}}}); err != nil {
				t.Fatalf("submit vitality: %v", err)
			}
			beforeAudit, err := svc.GetAudit(ctx, id)
			if err != nil {
				t.Fatalf("get baseline audit: %v", err)
			}
			if tc.prepareSQL != "" {
				if _, err := svc.Store().DB().ExecContext(ctx, tc.prepareSQL); err != nil {
					t.Fatalf("install failure trigger: %v", err)
				}
			}

			opID := "toxin-batch-" + tc.name
			if err := svc.SubmitToxin(ctx, id, opID, 1, tc.failed); err == nil {
				t.Fatal("expected toxin batch to fail")
			}
			evidence, err := svc.GetEvidence(ctx, id)
			if err != nil {
				t.Fatalf("get evidence after failure: %v", err)
			}
			if evidence.Toxin != 0 {
				t.Fatalf("failed batch left %d toxin evidence versions", evidence.Toxin)
			}
			afterAudit, err := svc.GetAudit(ctx, id)
			if err != nil {
				t.Fatalf("get audit after failure: %v", err)
			}
			if len(afterAudit) != len(beforeAudit) {
				t.Fatalf("failed batch left audit events: before=%d after=%d", len(beforeAudit), len(afterAudit))
			}
			current, err := svc.GetTask(ctx, id)
			if err != nil {
				t.Fatalf("get task after failure: %v", err)
			}
			if current.Status != "toxin_verifying" {
				t.Fatalf("failed batch advanced task to %q", current.Status)
			}
			var receipts int
			if err := svc.Store().DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM operation_receipts WHERE operation_id = ?`, opID).Scan(&receipts); err != nil {
				t.Fatalf("count failed-operation receipts: %v", err)
			}
			if receipts != 0 {
				t.Fatalf("failed batch left %d successful operation receipts", receipts)
			}

			if tc.prepareSQL != "" {
				if _, err := svc.Store().DB().ExecContext(ctx, `DROP TRIGGER IF EXISTS reject_h2_toxin; DROP TRIGGER IF EXISTS reject_toxin_audit`); err != nil {
					t.Fatalf("remove failure trigger: %v", err)
				}
			}
			if err := svc.SubmitToxin(ctx, id, opID, 1, validBatch); err != nil {
				t.Fatalf("corrected retry: %v", err)
			}
			if err := svc.SubmitToxin(ctx, id, opID, 1, validBatch); err != nil {
				t.Fatalf("idempotent replay: %v", err)
			}
			evidence, err = svc.GetEvidence(ctx, id)
			if err != nil {
				t.Fatalf("get evidence after retry and replay: %v", err)
			}
			if evidence.Toxin != 2 {
				t.Fatalf("retry and replay produced %d toxin versions, want 2", evidence.Toxin)
			}
			current, err = svc.GetTask(ctx, id)
			if err != nil {
				t.Fatalf("get task after retry: %v", err)
			}
			if current.Status != "pathogen_retesting" {
				t.Fatalf("complete toxin coverage left task in %q", current.Status)
			}
			finalAudit, err := svc.GetAudit(ctx, id)
			if err != nil {
				t.Fatalf("get final audit: %v", err)
			}
			if len(finalAudit) != len(beforeAudit)+1 {
				t.Fatalf("successful batch and replay added %d audit events, want 1", len(finalAudit)-len(beforeAudit))
			}
		})
	}
}
