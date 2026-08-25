package adjudication

import (
	"context"
	"errors"
	"sync"
)

// DeviceError wraps an instrument failure with its deterministic category so a
// caller can persist a precise, retryable failure rather than an opaque error.
type DeviceError struct {
	Category FailureCategory
	Err      error
}

func (e *DeviceError) Error() string {
	if e == nil || e.Err == nil {
		return string(e.Category)
	}
	return string(e.Category) + ": " + e.Err.Error()
}

func (e *DeviceError) Unwrap() error { return e.Err }

// Runner executes one device call and classifies the outcome. A nil error is a
// success; otherwise the failure category is taken from a typed DeviceError or
// inferred from the context error. The runner holds no database transaction:
// the caller persists the pending call first, invokes the adapter, then appends
// the result under a call-version guard.
type Runner struct {
	Adapter DeviceAdapter
}

// Call runs the adapter and returns the payload, the failure category (empty on
// success) and the original error.
func (r Runner) Call(ctx context.Context, req DeviceRequest) ([]byte, FailureCategory, error) {
	payload, err := r.Adapter.Call(ctx, req)
	if err == nil {
		return payload, "", nil
	}
	var de *DeviceError
	if errors.As(err, &de) {
		return nil, de.Category, err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, FailureTimeout, err
	}
	return nil, FailureDisconnected, err
}

// ScriptOutcome is one scripted device result: either a successful payload or a
// typed failure.
type ScriptOutcome struct {
	Payload []byte
	Failure FailureCategory
}

// ScriptAdapter is a deterministic, scriptable instrument boundary. It replays
// a fixed list of outcomes in order, which is what makes failure and retry
// tests reproducible without touching a real instrument.
type ScriptAdapter struct {
	mu       sync.Mutex
	outcomes []ScriptOutcome
	next     int
}

// NewScriptAdapter builds an adapter from an ordered outcome script.
func NewScriptAdapter(outcomes ...ScriptOutcome) *ScriptAdapter {
	return &ScriptAdapter{outcomes: outcomes}
}

// Call implements DeviceAdapter. Outcomes are consumed in order; once the
// script is exhausted the adapter reports a disconnected failure.
func (a *ScriptAdapter) Call(_ context.Context, _ DeviceRequest) ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.next < len(a.outcomes) {
		o := a.outcomes[a.next]
		a.next++
		if o.Failure != "" {
			return nil, &DeviceError{Category: o.Failure, Err: errors.New(string(o.Failure))}
		}
		return o.Payload, nil
	}
	return nil, &DeviceError{Category: FailureDisconnected, Err: errors.New("script exhausted")}
}

// StaticAdapter always returns the same payload, simulating a healthy device.
type StaticAdapter struct {
	Payload []byte
}

// Call implements DeviceAdapter.
func (a StaticAdapter) Call(_ context.Context, _ DeviceRequest) ([]byte, error) {
	return a.Payload, nil
}
