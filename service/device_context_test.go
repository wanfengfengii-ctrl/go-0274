package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"oyster-purification-release-gate/adjudication"
	"oyster-purification-release-gate/catalog"
	"oyster-purification-release-gate/domain"
	"oyster-purification-release-gate/store"
)

type contextBlockingAdapter struct {
	started chan struct{}
	stopped chan error
	release chan struct{}
}

func (a *contextBlockingAdapter) Call(ctx context.Context, _ adjudication.DeviceRequest) ([]byte, error) {
	close(a.started)
	select {
	case <-ctx.Done():
		a.stopped <- ctx.Err()
		return nil, ctx.Err()
	case <-a.release:
		return []byte(`{"norovirus_ct":"18.0"}`), nil
	}
}

func TestModel_DeviceCallHonorsRequestContext(t *testing.T) {
	cases := []struct {
		name       string
		newContext func() (context.Context, context.CancelFunc)
		interrupt  func(context.CancelFunc)
		wantErr    error
	}{
		{
			name: "explicit cancellation",
			newContext: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			interrupt: func(cancel context.CancelFunc) { cancel() },
			wantErr:   context.Canceled,
		},
		{
			name: "deadline expiration",
			newContext: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 500*time.Millisecond)
			},
			interrupt: func(context.CancelFunc) {},
			wantErr:   context.DeadlineExceeded,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, err := store.Open(context.Background(), ":memory:", domain.FixedClock(0))
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = st.Close() })

			adapter := &contextBlockingAdapter{
				started: make(chan struct{}),
				stopped: make(chan error, 1),
				release: make(chan struct{}),
			}
			svc := New(st, catalog.DefaultCatalog(), adapter)
			id := mustLock(t, svc, "POOL-CONTEXT-"+tc.name)
			mustAdvance(t, svc, id)

			ctx, cancel := tc.newContext()
			defer cancel()
			result := make(chan error, 1)
			go func() {
				_, err := svc.StartDeviceCall(ctx, id, "op-device-context", 1, DeviceCallRequest{Hole: "H2", Kind: "qpcr"})
				result <- err
			}()

			select {
			case <-adapter.started:
			case <-time.After(2 * time.Second):
				t.Fatal("device adapter was not invoked")
			}
			tc.interrupt(cancel)

			select {
			case got := <-adapter.stopped:
				if !errors.Is(got, tc.wantErr) {
					t.Errorf("adapter stopped with %v, want %v", got, tc.wantErr)
				}
			case <-time.After(time.Second):
				close(adapter.release)
				select {
				case <-result:
				case <-time.After(2 * time.Second):
				}
				t.Fatalf("request context did not stop the in-flight device call")
			}

			select {
			case got := <-result:
				if !errors.Is(got, tc.wantErr) {
					t.Fatalf("StartDeviceCall returned %v, want %v", got, tc.wantErr)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("StartDeviceCall did not return after its adapter stopped")
			}
		})
	}
}
