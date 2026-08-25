package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"oyster-purification-release-gate/adjudication"
	"oyster-purification-release-gate/catalog"
	"oyster-purification-release-gate/domain"
	"oyster-purification-release-gate/store"
)

type modelStartAdapter struct {
	mu         sync.Mutex
	requests   []adjudication.DeviceRequest
	blockFirst bool
	entered    chan struct{}
	release    chan struct{}
}

func (a *modelStartAdapter) Call(_ context.Context, req adjudication.DeviceRequest) ([]byte, error) {
	a.mu.Lock()
	a.requests = append(a.requests, req)
	call := len(a.requests)
	a.mu.Unlock()
	if a.blockFirst && call == 1 {
		close(a.entered)
		<-a.release
	}
	return nil, &adjudication.DeviceError{Category: adjudication.FailureTimeout, Err: context.DeadlineExceeded}
}

func (a *modelStartAdapter) snapshot() []adjudication.DeviceRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]adjudication.DeviceRequest(nil), a.requests...)
}

func TestModel_DeviceStartReusesExistingCall(t *testing.T) {
	tests := []struct {
		name            string
		blockFirst      bool
		resubmit        bool
		wantRetryError  bool
		wantDeviceCalls int
	}{
		{name: "new target still starts attempt one", wantDeviceCalls: 1},
		{name: "failed target returns stable retry state", resubmit: true, wantRetryError: true, wantDeviceCalls: 1},
		{name: "pending target returns stable retry state", blockFirst: true, resubmit: true, wantRetryError: true, wantDeviceCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			st, err := store.Open(ctx, t.TempDir()+"/device-start.db", domain.FixedClock(0))
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = st.Close() })
			adapter := &modelStartAdapter{blockFirst: tt.blockFirst}
			if tt.blockFirst {
				adapter.entered = make(chan struct{})
				adapter.release = make(chan struct{})
			}
			svc := New(st, catalog.DefaultCatalog(), adapter)
			id := mustLock(t, svc, "MODEL-"+tt.name)
			mustAdvance(t, svc, id)

			req := DeviceCallRequest{Hole: "H2", Kind: "qpcr"}
			var firstResp *DeviceCallResponse
			var firstErr error
			firstDone := make(chan struct{})
			if tt.blockFirst {
				go func() {
					firstResp, firstErr = svc.StartDeviceCall(ctx, id, "op-first", 1, req)
					close(firstDone)
				}()
				<-adapter.entered
			} else {
				firstResp, firstErr = svc.StartDeviceCall(ctx, id, "op-first", 1, req)
			}

			var retryErr error
			if tt.resubmit {
				_, retryErr = svc.StartDeviceCall(ctx, id, "op-duplicate", 1, req)
			}
			if tt.blockFirst {
				close(adapter.release)
				<-firstDone
			}

			if firstErr != nil {
				t.Fatalf("first start returned error: %v", firstErr)
			}
			if firstResp == nil || firstResp.Status != "failed" {
				t.Fatalf("first start = %#v, want persisted failed call", firstResp)
			}
			if tt.wantRetryError {
				var apiErr *domain.APIError
				if !errors.As(retryErr, &apiErr) || apiErr.Code != domain.CodeDeviceRetryPending {
					t.Errorf("duplicate start error = %v, want %s", retryErr, domain.CodeDeviceRetryPending)
				}
			} else if retryErr != nil {
				t.Errorf("unexpected duplicate start error: %v", retryErr)
			}

			requests := adapter.snapshot()
			if len(requests) != tt.wantDeviceCalls {
				t.Errorf("device invoked %d times, want %d; requests=%v", len(requests), tt.wantDeviceCalls, requests)
			}
			for i, got := range requests {
				if got.Attempt != 1 {
					t.Errorf("device request %d attempt = %d, want 1", i, got.Attempt)
				}
			}

			var rows, maxAttempt int
			if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(attempt), 0) FROM device_attempts`).Scan(&rows, &maxAttempt); err != nil {
				t.Fatalf("query attempts: %v", err)
			}
			if rows != 1 || maxAttempt != 1 {
				t.Errorf("persisted attempts = (rows=%d, max=%d), want exactly attempt 1", rows, maxAttempt)
			}
		})
	}
}
