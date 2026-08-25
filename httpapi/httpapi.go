// Package httpapi implements the Go HTTP API component: strict JSON, stable
// error responses, routing and health checks. It connects the application
// services to the persistence transactions and carries no bypass write path.
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"oyster-purification-release-gate/domain"
)

// Health is the dependency queried by GET /healthz.
type Health interface {
	// Healthy reports whether the process and its dependencies are ready.
	Healthy() bool
}

// WriteError renders an error as the stable {code, message, reasons} JSON body.
// Unknown error values are translated to a generic internal error so the wire
// contract never leaks implementation details.
func WriteError(w http.ResponseWriter, err error) {
	var apiErr *domain.APIError
	if !errors.As(err, &apiErr) {
		apiErr = &domain.APIError{
			Code:    "INTERNAL",
			Message: "internal error",
		}
	}
	writeJSON(w, httpStatusFor(apiErr.Code), apiErr)
}

func httpStatusFor(code domain.ErrorCode) int {
	switch code {
	case "NOT_FOUND":
		return http.StatusNotFound
	case "BAD_REQUEST":
		return http.StatusBadRequest
	case domain.CodeOperationConflict, domain.CodeResourceBusy,
		domain.CodeCoverageInvalid, domain.CodeRoleConflict,
		domain.CodeDuplicateSeal, domain.CodeDuplicateBlindCode,
		domain.CodeAreaTideMismatch, domain.CodeStaleRuleDigest,
		domain.CodeFixedPointOverflow, domain.CodeStaleGeneration,
		domain.CodeTerminalState, domain.CodeDeviceRetryPending:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
