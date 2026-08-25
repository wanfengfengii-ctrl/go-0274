package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"oyster-purification-release-gate/adjudication"
	"oyster-purification-release-gate/catalog"
	"oyster-purification-release-gate/domain"
	"oyster-purification-release-gate/httpapi"
	"oyster-purification-release-gate/service"
	"oyster-purification-release-gate/store"
)

func TestModel_DeviceRetryTimeGate(t *testing.T) {
	tests := []struct {
		name  string
		drive func(*testing.T, *service.Service, http.Handler, *domain.SteppingClock, int64)
	}{
		{
			name: "manual retry endpoint",
			drive: func(t *testing.T, svc *service.Service, handler http.Handler, clock *domain.SteppingClock, callID int64) {
				early := httptest.NewRecorder()
				handler.ServeHTTP(early, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/v1/device-calls/%d/retry", callID), nil))
				if early.Code != http.StatusConflict {
					t.Fatalf("early retry status = %d, want %d; body=%s", early.Code, http.StatusConflict, early.Body.String())
				}
				var body struct {
					Code domain.ErrorCode `json:"code"`
				}
				if err := json.Unmarshal(early.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode early retry response: %v", err)
				}
				if body.Code != domain.CodeDeviceRetryPending {
					t.Fatalf("early retry code = %q, want %q", body.Code, domain.CodeDeviceRetryPending)
				}

				clock.SetTime(1)
				due := httptest.NewRecorder()
				handler.ServeHTTP(due, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/v1/device-calls/%d/retry", callID), nil))
				if due.Code != http.StatusOK {
					t.Fatalf("due retry status = %d, want %d; body=%s", due.Code, http.StatusOK, due.Body.String())
				}
			},
		},
		{
			name: "recovery scanner",
			drive: func(t *testing.T, svc *service.Service, _ http.Handler, _ *domain.SteppingClock, _ int64) {
				if driven, err := svc.Recover(context.Background(), 0); err != nil || driven != 0 {
					t.Fatalf("early Recover = (%d, %v), want (0, nil)", driven, err)
				}
				if driven, err := svc.Recover(context.Background(), 1); err != nil || driven != 1 {
					t.Fatalf("due Recover = (%d, %v), want (1, nil)", driven, err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			clock := domain.NewSteppingClock(0, 1)
			st, err := store.Open(ctx, ":memory:", clock)
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = st.Close() })

			adapter := adjudication.NewScriptAdapter(
				adjudication.ScriptOutcome{Failure: adjudication.FailureTimeout},
				adjudication.ScriptOutcome{Payload: []byte(`{"norovirus_ct":"18.0"}`)},
			)
			svc := service.New(st, catalog.DefaultCatalog(), adapter)
			handler := httpapi.NewHandler(svc, nil)
			snapshot := catalog.DefaultSnapshot()
			taskResp, err := svc.CreateTask(ctx, service.CreateTaskRequest{
				AreaID: "east", RuleSnapshotID: catalog.DefaultSnapshotID, RuleDigest: snapshot.Digest,
			})
			if err != nil {
				t.Fatalf("create task: %v", err)
			}
			taskID := domain.TaskID(taskResp.ID)
			if _, err := svc.LockTask(ctx, taskID, "op-lock", 1, service.LockRequest{
				TideBatch: "T1", UVLampBatch: "UV1", LockedCount: 10,
				CageSeals: []string{"C1"}, Timepoints: []string{"TP1"}, ObservationPoints: []string{"OP1"},
				BlindCodes: []string{"B1", "B2", "B3"}, Pool: "retry-gate-" + tt.name,
				PumpBranch: "PU1", ProbeWindow: "PR1",
				Holes:     []service.HoleSpec{{ID: "H1", Kind: "toxin"}, {ID: "H2", Kind: "qpcr"}},
				Reviewers: []string{"R1", "R2"},
			}); err != nil {
				t.Fatalf("lock task: %v", err)
			}
			if err := svc.ConfirmIntake(ctx, taskID, "op-intake", 1, service.IntakeRequest{Confirmers: []string{"C1", "C2"}}); err != nil {
				t.Fatalf("confirm intake: %v", err)
			}
			if err := svc.SealSamples(ctx, taskID, "op-seal", 1, service.SealRequest{Seals: []service.SealSpec{{CageSeal: "C1", Location: "L1", BlindCodes: []string{"B1", "B2", "B3"}}}}); err != nil {
				t.Fatalf("seal samples: %v", err)
			}
			if err := svc.SubmitVitality(ctx, taskID, "op-vitality", 1, service.VitalityRequest{Cells: []service.VitalityCellInput{{CageSeal: "C1", TimepointID: "TP1", ObservationPointID: "OP1", Closed: 10}}}); err != nil {
				t.Fatalf("submit vitality: %v", err)
			}
			if err := svc.SubmitToxin(ctx, taskID, "op-toxin", 1, service.ToxinRequest{Readings: []service.ToxinInput{{Hole: "H1", PSPRaw: "0.1", DSPRaw: "0.1", Factor: 1, Divisor: 1}}}); err != nil {
				t.Fatalf("submit toxin: %v", err)
			}
			call, err := svc.StartDeviceCall(ctx, taskID, "op-device", 1, service.DeviceCallRequest{Hole: "H2", Kind: "qpcr"})
			if err != nil || call.Status != "failed" {
				t.Fatalf("initial device call = (%+v, %v), want failed", call, err)
			}

			assertState := func(wantAttempts int, wantStatus string, wantEvidence int, wantOrder []int) {
				t.Helper()
				var attempts, evidence int
				var status string
				var order []int
				err := st.Tx(ctx, func(tx *store.Tx) error {
					if err := tx.QueryRowContext(ctx, `SELECT attempts, status FROM device_calls WHERE id = ?`, call.CallID).Scan(&attempts, &status); err != nil {
						return err
					}
					if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pathogen_evidence_versions WHERE task_id = ?`, taskID).Scan(&evidence); err != nil {
						return err
					}
					rows, err := tx.QueryContext(ctx, `SELECT attempt FROM device_attempts WHERE call_id = ? ORDER BY attempt`, call.CallID)
					if err != nil {
						return err
					}
					defer rows.Close()
					for rows.Next() {
						var attempt int
						if err := rows.Scan(&attempt); err != nil {
							return err
						}
						order = append(order, attempt)
					}
					return rows.Err()
				})
				if err != nil {
					t.Fatalf("read persisted device state: %v", err)
				}
				if attempts != wantAttempts || status != wantStatus || evidence != wantEvidence || !reflect.DeepEqual(order, wantOrder) {
					t.Fatalf("state = attempts:%d status:%q evidence:%d order:%v, want attempts:%d status:%q evidence:%d order:%v", attempts, status, evidence, order, wantAttempts, wantStatus, wantEvidence, wantOrder)
				}
			}

			assertState(1, "failed", 0, []int{1})
			tt.drive(t, svc, handler, clock, call.CallID)
			assertState(2, "success", 1, []int{1, 2})
		})
	}
}
