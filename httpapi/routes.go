package httpapi

import (
	"net/http"

	"oyster-purification-release-gate/domain"
	"oyster-purification-release-gate/service"
)

// Handler wires the HTTP routes to the application service. It carries no
// bypass write path: every mutating route funnels through a service method that
// runs inside a transaction.
type Handler struct {
	svc    *service.Service
	health Health
}

// NewHandler assembles the full route table.
func NewHandler(svc *service.Service, health Health) http.Handler {
	h := &Handler{svc: svc, health: health}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("POST /v1/tasks", h.createTask)
	mux.HandleFunc("POST /v1/tasks/{id}/lock", h.lockTask)
	mux.HandleFunc("POST /v1/tasks/{id}/intake-confirmations", h.confirmIntake)
	mux.HandleFunc("POST /v1/tasks/{id}/sample-seals", h.sealSamples)
	mux.HandleFunc("POST /v1/tasks/{id}/vitality", h.submitVitality)
	mux.HandleFunc("POST /v1/tasks/{id}/water-readings", h.submitWater)
	mux.HandleFunc("POST /v1/tasks/{id}/toxin-readings", h.submitToxin)
	mux.HandleFunc("POST /v1/tasks/{id}/device-calls", h.startDeviceCall)
	mux.HandleFunc("POST /v1/device-calls/{callID}/retry", h.retryDeviceCall)
	mux.HandleFunc("POST /v1/device-callbacks", h.deviceCallback)
	mux.HandleFunc("POST /v1/tasks/{id}/rechecks", h.createRecheck)
	mux.HandleFunc("POST /v1/tasks/{id}/blind-reveal", h.revealBlindCode)
	mux.HandleFunc("POST /v1/tasks/{id}/reviews", h.submitReview)
	mux.HandleFunc("POST /v1/tasks/{id}/decisions/release", h.decide("release"))
	mux.HandleFunc("POST /v1/tasks/{id}/decisions/isolate", h.decide("isolate"))
	mux.HandleFunc("POST /v1/tasks/{id}/decisions/cancel", h.decide("cancel"))
	mux.HandleFunc("POST /v1/tasks/{id}/dispatch", h.dispatch)
	mux.HandleFunc("GET /v1/tasks/{id}", h.getTask)
	mux.HandleFunc("GET /v1/tasks/{id}/coverage", h.getCoverage)
	mux.HandleFunc("GET /v1/tasks/{id}/evidence", h.getEvidence)
	mux.HandleFunc("GET /v1/tasks/{id}/audit", h.getAudit)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, &domain.APIError{Code: "NOT_FOUND", Message: "unknown route"})
	})
	return mux
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	code := http.StatusOK
	if h.health == nil || !h.health.Healthy() {
		status = "degraded"
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]string{"status": status})
}

func (h *Handler) createTask(w http.ResponseWriter, r *http.Request) {
	var req service.CreateTaskRequest
	if err := decodeStrict(w, r, &req); err != nil {
		WriteError(w, err)
		return
	}
	resp, err := h.svc.CreateTask(r.Context(), req)
	if err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) lockTask(w http.ResponseWriter, r *http.Request) {
	var req service.LockRequest
	if err := decodeStrict(w, r, &req); err != nil {
		WriteError(w, err)
		return
	}
	resp, err := h.svc.LockTask(r.Context(), taskIDParam(r), operationID(r), generation(r), req)
	if err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) confirmIntake(w http.ResponseWriter, r *http.Request) {
	var req service.IntakeRequest
	if err := decodeStrict(w, r, &req); err != nil {
		WriteError(w, err)
		return
	}
	if err := h.svc.ConfirmIntake(r.Context(), taskIDParam(r), operationID(r), generation(r), req); err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "confirmed"})
}

func (h *Handler) sealSamples(w http.ResponseWriter, r *http.Request) {
	var req service.SealRequest
	if err := decodeStrict(w, r, &req); err != nil {
		WriteError(w, err)
		return
	}
	if err := h.svc.SealSamples(r.Context(), taskIDParam(r), operationID(r), generation(r), req); err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "sealed"})
}

func (h *Handler) submitVitality(w http.ResponseWriter, r *http.Request) {
	var req service.VitalityRequest
	if err := decodeStrict(w, r, &req); err != nil {
		WriteError(w, err)
		return
	}
	if err := h.svc.SubmitVitality(r.Context(), taskIDParam(r), operationID(r), generation(r), req); err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

func (h *Handler) submitWater(w http.ResponseWriter, r *http.Request) {
	var req service.WaterRequest
	if err := decodeStrict(w, r, &req); err != nil {
		WriteError(w, err)
		return
	}
	if err := h.svc.SubmitWater(r.Context(), taskIDParam(r), operationID(r), generation(r), req); err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

func (h *Handler) submitToxin(w http.ResponseWriter, r *http.Request) {
	var req service.ToxinRequest
	if err := decodeStrict(w, r, &req); err != nil {
		WriteError(w, err)
		return
	}
	if err := h.svc.SubmitToxin(r.Context(), taskIDParam(r), operationID(r), generation(r), req); err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

func (h *Handler) startDeviceCall(w http.ResponseWriter, r *http.Request) {
	var req service.DeviceCallRequest
	if err := decodeStrict(w, r, &req); err != nil {
		WriteError(w, err)
		return
	}
	resp, err := h.svc.StartDeviceCall(r.Context(), taskIDParam(r), operationID(r), generation(r), req)
	if err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) retryDeviceCall(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.RetryDeviceCall(r.Context(), callIDParam(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) deviceCallback(w http.ResponseWriter, r *http.Request) {
	var req service.DeviceCallbackRequest
	if err := decodeStrict(w, r, &req); err != nil {
		WriteError(w, err)
		return
	}
	if err := h.svc.DeviceCallback(r.Context(), req); err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

func (h *Handler) createRecheck(w http.ResponseWriter, r *http.Request) {
	var req service.RecheckRequest
	if err := decodeStrict(w, r, &req); err != nil {
		WriteError(w, err)
		return
	}
	if err := h.svc.CreateRecheck(r.Context(), taskIDParam(r), operationID(r), generation(r), req); err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "recheck-created"})
}

func (h *Handler) revealBlindCode(w http.ResponseWriter, r *http.Request) {
	var req service.RevealRequest
	if err := decodeStrict(w, r, &req); err != nil {
		WriteError(w, err)
		return
	}
	sampleID, err := h.svc.RevealBlindCode(r.Context(), operationID(r), req)
	if err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"sample_id": sampleID})
}

func (h *Handler) submitReview(w http.ResponseWriter, r *http.Request) {
	var req service.ReviewRequest
	if err := decodeStrict(w, r, &req); err != nil {
		WriteError(w, err)
		return
	}
	if err := h.svc.SubmitReview(r.Context(), taskIDParam(r), operationID(r), generation(r), req); err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "reviewed"})
}

func (h *Handler) decide(decision string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req service.DecisionRequest
		if err := decodeStrict(w, r, &req); err != nil {
			WriteError(w, err)
			return
		}
		resp, err := h.svc.Decide(r.Context(), taskIDParam(r), operationID(r), generation(r), decision, req)
		if err != nil {
			WriteError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func (h *Handler) dispatch(w http.ResponseWriter, r *http.Request) {
	var req service.DispatchRequest
	if err := decodeStrict(w, r, &req); err != nil {
		WriteError(w, err)
		return
	}
	resp, err := h.svc.Dispatch(r.Context(), taskIDParam(r), operationID(r), generation(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) getTask(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.GetTask(r.Context(), taskIDParam(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) getCoverage(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.GetCoverage(r.Context(), taskIDParam(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) getEvidence(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.GetEvidence(r.Context(), taskIDParam(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) getAudit(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.GetAudit(r.Context(), taskIDParam(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
