package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"oyster-purification-release-gate/domain"
)

// maxBodyBytes caps the request body size. The failure boundary requires a
// strict size limit before any business transaction opens.
const maxBodyBytes = 1 << 20

// decodeStrict decodes a JSON body, rejecting unknown fields, trailing data and
// oversized bodies. It returns a stable APIError on any violation.
func decodeStrict(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return &domain.APIError{Code: "BAD_REQUEST", Message: "invalid request body: " + err.Error()}
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return &domain.APIError{Code: "BAD_REQUEST", Message: "request body must contain a single JSON object"}
	}
	return nil
}

// operationID returns the idempotency key from the Operation-Id header.
func operationID(r *http.Request) string { return r.Header.Get("Operation-Id") }

// generation returns the task generation from the If-Task-Generation header,
// defaulting to zero when absent or malformed.
func generation(r *http.Request) domain.Generation {
	s := r.Header.Get("If-Task-Generation")
	g, _ := strconv.ParseInt(s, 10, 64)
	return domain.Generation(g)
}

// taskIDParam extracts a path value set by the router.
func taskIDParam(r *http.Request) domain.TaskID {
	return domain.TaskID(r.PathValue("id"))
}

// callIDParam extracts a numeric path value.
func callIDParam(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("callID"), 10, 64)
	return id
}
