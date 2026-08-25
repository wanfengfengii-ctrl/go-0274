package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"oyster-purification-release-gate/adjudication"
	"oyster-purification-release-gate/catalog"
	"oyster-purification-release-gate/domain"
	"oyster-purification-release-gate/service"
	"oyster-purification-release-gate/store"
)

type alwaysHealthy struct{}

func (alwaysHealthy) Healthy() bool { return true }

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	st, err := store.Open(context.Background(), ":memory:", domain.FixedClock(0))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc := service.New(st, catalog.DefaultCatalog(), adjudication.StaticAdapter{Payload: []byte(`{"norovirus_ct":"18.0","coliform":"2"}`)})
	return NewHandler(svc, alwaysHealthy{})
}

func TestHealthzOK(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestHandler(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("unexpected status: %v", body)
	}
}

func TestUnknownRouteReturnsStableError(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestHandler(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Code != "NOT_FOUND" {
		t.Fatalf("unexpected code: %v", body.Code)
	}
}

func TestWriteErrorStableShape(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, domain.NewError(domain.CodeResourceBusy, "pool occupied"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
	var body struct {
		Code    string   `json:"code"`
		Message string   `json:"message"`
		Reasons []string `json:"reasons"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Code != "RESOURCE_BUSY" || body.Message != "pool occupied" {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestWriteErrorMapsUnknownToInternal(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, errBoom{})
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "INTERNAL" {
		t.Fatalf("expected INTERNAL, got %v", body["code"])
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }
