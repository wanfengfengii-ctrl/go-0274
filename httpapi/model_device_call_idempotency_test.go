package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"oyster-purification-release-gate/adjudication"
	"oyster-purification-release-gate/catalog"
	"oyster-purification-release-gate/domain"
	"oyster-purification-release-gate/httpapi"
	"oyster-purification-release-gate/service"
	"oyster-purification-release-gate/store"
)

func TestModel_DeviceCallIdempotentReplayAfterTimeout(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, ":memory:", domain.FixedClock(0))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	adapter := adjudication.NewScriptAdapter(
		adjudication.ScriptOutcome{Failure: adjudication.FailureTimeout},
		adjudication.ScriptOutcome{Payload: []byte(`{"norovirus_ct":"18.0"}`)},
	)
	svc := service.New(st, catalog.DefaultCatalog(), adapter)
	task, err := svc.CreateTask(ctx, service.CreateTaskRequest{
		AreaID:         "east",
		RuleSnapshotID: catalog.DefaultSnapshotID,
		RuleDigest:     catalog.DefaultSnapshot().Digest,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	taskID := domain.TaskID(task.ID)
	_, err = svc.LockTask(ctx, taskID, "setup-lock", 1, service.LockRequest{
		TideBatch: "T1", UVLampBatch: "UV1", LockedCount: 10,
		CageSeals: []string{"C1"}, Timepoints: []string{"TP1"}, ObservationPoints: []string{"OP1"},
		BlindCodes: []string{"B1", "B2", "B3"}, Pool: "POOL-MODEL", PumpBranch: "PU1", ProbeWindow: "PR1",
		Holes:     []service.HoleSpec{{ID: "H1", Kind: "toxin"}, {ID: "H2", Kind: "qpcr"}, {ID: "H3", Kind: "culture"}},
		Reviewers: []string{"R1", "R2"},
	})
	if err != nil {
		t.Fatalf("lock task: %v", err)
	}
	if err := svc.ConfirmIntake(ctx, taskID, "setup-intake", 1, service.IntakeRequest{Confirmers: []string{"C1", "C2"}}); err != nil {
		t.Fatalf("confirm intake: %v", err)
	}
	if err := svc.SealSamples(ctx, taskID, "setup-seal", 1, service.SealRequest{Seals: []service.SealSpec{{CageSeal: "C1", Location: "L1", BlindCodes: []string{"B1", "B2", "B3"}}}}); err != nil {
		t.Fatalf("seal samples: %v", err)
	}
	if err := svc.SubmitVitality(ctx, taskID, "setup-vitality", 1, service.VitalityRequest{Cells: []service.VitalityCellInput{{CageSeal: "C1", TimepointID: "TP1", ObservationPointID: "OP1", Closed: 10}}}); err != nil {
		t.Fatalf("submit vitality: %v", err)
	}
	if err := svc.SubmitToxin(ctx, taskID, "setup-toxin", 1, service.ToxinRequest{Readings: []service.ToxinInput{{Hole: "H1", PSPRaw: "0.1", DSPRaw: "0.1", Factor: 1, Divisor: 1}}}); err != nil {
		t.Fatalf("submit toxin: %v", err)
	}

	handler := httpapi.NewHandler(svc, nil)
	devicePath := fmt.Sprintf("/v1/tasks/%s/device-calls", task.ID)
	type response struct {
		CallID int64  `json:"call_id"`
		Status string `json:"status"`
		Hole   string `json:"hole"`
		Kind   string `json:"kind"`
		Code   string `json:"code"`
	}
	var first response

	cases := []struct {
		name       string
		path       func() string
		operation  string
		body       string
		wantStatus int
		check      func(*testing.T, response)
	}{
		{
			name: "initial timeout is a stable retryable failure",
			path: func() string { return devicePath }, operation: "device-op",
			body: `{"hole":"H2","kind":"qpcr"}`, wantStatus: http.StatusOK,
			check: func(t *testing.T, got response) {
				if got.CallID == 0 || got.Status != "failed" || got.Hole != "H2" || got.Kind != "qpcr" {
					t.Fatalf("unexpected initial response: %+v", got)
				}
				first = got
			},
		},
		{
			name: "same operation and body replays the failed call",
			path: func() string { return devicePath }, operation: "device-op",
			body: `{"hole":"H2","kind":"qpcr"}`, wantStatus: http.StatusOK,
			check: func(t *testing.T, got response) {
				if got != first {
					t.Fatalf("replay changed first result: first=%+v replay=%+v", first, got)
				}
			},
		},
		{
			name: "same operation with different body conflicts",
			path: func() string { return devicePath }, operation: "device-op",
			body: `{"hole":"H3","kind":"incubator"}`, wantStatus: http.StatusConflict,
			check: func(t *testing.T, got response) {
				if got.Code != string(domain.CodeOperationConflict) {
					t.Fatalf("expected OPERATION_CONFLICT, got %+v", got)
				}
			},
		},
		{
			name:       "persisted timeout call can be retried",
			path:       func() string { return fmt.Sprintf("/v1/device-calls/%d/retry", first.CallID) },
			wantStatus: http.StatusOK,
			check: func(t *testing.T, got response) {
				if got.CallID != first.CallID || got.Status != "success" {
					t.Fatalf("unexpected retry response: %+v", got)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path(), bytes.NewBufferString(tc.body))
			if tc.operation != "" {
				req.Header.Set("Operation-Id", tc.operation)
				req.Header.Set("If-Task-Generation", "1")
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("expected HTTP %d, got %d: %s", tc.wantStatus, rec.Code, rec.Body.String())
			}
			var got response
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			tc.check(t, got)
		})
	}
}
