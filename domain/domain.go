// Package domain holds the stable value types, identifiers, logical clock and
// the single stable error structure shared by every other package.
//
// It implements the "稳定错误结构" failure boundary (code / message / reasons)
// and the injectable logical clock required for deterministic concurrency and
// failure tests described in the project document.
package domain

// ErrorCode is a stable, machine-readable error discriminator. The complete
// set is defined by the project's failure boundary contract.
type ErrorCode string

const (
	CodeAreaTideMismatch   ErrorCode = "AREA_TIDE_MISMATCH"
	CodeStaleRuleDigest    ErrorCode = "STALE_RULE_DIGEST"
	CodeDuplicateSeal      ErrorCode = "DUPLICATE_SEAL"
	CodeDuplicateBlindCode ErrorCode = "DUPLICATE_BLIND_CODE"
	CodeResourceBusy       ErrorCode = "RESOURCE_BUSY"
	CodeCoverageInvalid    ErrorCode = "COVERAGE_INVALID"
	CodeFixedPointOverflow ErrorCode = "FIXED_POINT_OVERFLOW"
	CodeDeviceRetryPending ErrorCode = "DEVICE_RETRY_PENDING"
	CodeRoleConflict       ErrorCode = "ROLE_CONFLICT"
	CodeOperationConflict  ErrorCode = "OPERATION_CONFLICT"
	CodeStaleGeneration    ErrorCode = "STALE_GENERATION"
	CodeTerminalState      ErrorCode = "TERMINAL_STATE"
)

// APIError is the single stable error structure returned by the HTTP API and
// carried through every business boundary. Reasons are deterministic: they are
// ordered by resource category before being emitted.
type APIError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Reasons []string  `json:"reasons,omitempty"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return string(e.Code) + ": " + e.Message
}

// NewError builds an APIError with no reasons.
func NewError(code ErrorCode, message string) *APIError {
	return &APIError{Code: code, Message: message}
}

// WithReason returns a shallow copy of the error with one additional reason.
func (e *APIError) WithReason(reason string) *APIError {
	if e == nil {
		return NewError("", "").WithReason(reason)
	}
	out := *e
	out.Reasons = append(append([]string(nil), e.Reasons...), reason)
	return &out
}

// LogicalTime is an injectable monotonic clock value. All lease expirations,
// retry scheduling and audit ordering use this integer, never wall-clock time.
type LogicalTime int64

// Generation identifies one task generation. Commands carry the generation they
// target so stale writes can be rejected without touching business data.
type Generation int64

// TaskID identifies a release task.
type TaskID string

// OperationID is the idempotency key attached to every mutating command.
type OperationID string

// PersonID identifies an operator with catalog-defined qualifications.
type PersonID string
