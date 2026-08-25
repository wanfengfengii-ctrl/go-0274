// Package adjudication implements the 病原复判及终局仲裁器 component: it
// orchestrates scriptable device calls, deterministic retry scheduling and the
// final release/isolate/cancel arbitration.
package adjudication

import (
	"context"

	"oyster-purification-release-gate/domain"
)

// DeviceKind identifies an instrument family (qPCR, toxin plate reader,
// incubator, water-quality probe).
type DeviceKind string

const (
	DeviceQPCR        DeviceKind = "qpcr"
	DevicePlateReader DeviceKind = "plate_reader"
	DeviceIncubator   DeviceKind = "incubator"
	DeviceProbe       DeviceKind = "probe"
)

// FailureCategory classifies a device call outcome for deterministic retries.
type FailureCategory string

const (
	FailureRejected     FailureCategory = "rejected"
	FailureDisconnected FailureCategory = "disconnected"
	FailureTimeout      FailureCategory = "timeout"
	FailureMalformed    FailureCategory = "malformed"
)

// DeviceRequest is the target of one device call.
type DeviceRequest struct {
	Kind       DeviceKind
	Hole       string
	Generation domain.Generation
	Attempt    int
}

// DeviceAdapter is a scriptable instrument boundary. Implementations simulate
// qPCR, plate-reader, incubator and probe responses for deterministic tests.
type DeviceAdapter interface {
	Call(ctx context.Context, req DeviceRequest) ([]byte, error)
}

// NextRetryAfter computes the next retry logical time deterministically from
// the current attempt number. The backoff is exponential and overflow-guarded
// so retry scheduling never depends on wall-clock time or goroutine ordering.
func NextRetryAfter(now domain.LogicalTime, attempt int) domain.LogicalTime {
	if attempt < 0 {
		attempt = 0
	}
	// shift == 2^attempt, capped to avoid overflow past 2^30.
	shift := 1
	for i := 0; i < attempt && i < 30; i++ {
		shift <<= 1
	}
	return now + domain.LogicalTime(shift)
}
