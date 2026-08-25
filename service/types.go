// Package service is the application layer that orchestrates the full release
// flow. It wires the pure business rules in catalog, task, ledger and
// adjudication to the persisted store, and is the single place where every
// business command runs inside one transaction (or, for device calls, a
// transaction-free instrument interaction bracketed by two transactions).
package service

// CreateTaskRequest is the body of POST /v1/tasks.
type CreateTaskRequest struct {
	AreaID         string `json:"area_id"`
	RuleSnapshotID string `json:"rule_snapshot_id"`
	RuleDigest     string `json:"rule_digest"`
}

// HoleSpec identifies one detection hole and its instrument kind.
type HoleSpec struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // "toxin" | "qpcr" | "culture"
}

// LockRequest is the body of POST /v1/tasks/{id}/lock. Everything here becomes
// an immutable snapshot once the lock commits.
type LockRequest struct {
	TideBatch         string     `json:"tide_batch"`
	UVLampBatch       string     `json:"uv_lamp_batch"`
	LockedCount       int64      `json:"locked_count"`
	CageSeals         []string   `json:"cage_seals"`
	Timepoints        []string   `json:"timepoints"`
	ObservationPoints []string   `json:"observation_points"`
	BlindCodes        []string   `json:"blind_codes"`
	Pool              string     `json:"pool"`
	PumpBranch        string     `json:"pump_branch"`
	ProbeWindow       string     `json:"probe_window"`
	Holes             []HoleSpec `json:"holes"`
	Reviewers         []string   `json:"reviewers"`
}

// IntakeRequest is the body of POST /v1/tasks/{id}/intake-confirmations.
type IntakeRequest struct {
	Confirmers []string `json:"confirmers"`
}

// SealRequest is the body of POST /v1/tasks/{id}/sample-seals.
type SealRequest struct {
	Seals []SealSpec `json:"seals"`
}

// SealSpec seals one cage: its location plus the three blind codes mapped to
// the cage's triplicate samples.
type SealSpec struct {
	CageSeal   string   `json:"cage_seal"`
	Location   string   `json:"location"`
	BlindCodes []string `json:"blind_codes"`
}

// VitalityCellInput is one vitality cell in a submission.
type VitalityCellInput struct {
	CageSeal           string `json:"cage_seal"`
	TimepointID        string `json:"timepoint_id"`
	ObservationPointID string `json:"observation_point_id"`
	Closed             int64  `json:"closed"`
	WeakOpen           int64  `json:"weak_open"`
	Dead               int64  `json:"dead"`
	Broken             int64  `json:"broken"`
	SupplementOf       string `json:"supplement_of,omitempty"`
}

// VitalityRequest is the body of POST /v1/tasks/{id}/vitality. Batch
// submission is all-or-nothing.
type VitalityRequest struct {
	Cells []VitalityCellInput `json:"cells"`
}

// WaterInput is one water-quality reading.
type WaterInput struct {
	CageSeal    string `json:"cage_seal"`
	TimepointID string `json:"timepoint_id"`
	Turbidity   string `json:"turbidity"`
	Salinity    string `json:"salinity"`
	Temperature string `json:"temperature"`
	Chlorine    string `json:"chlorine"`
}

// WaterRequest is the body of POST /v1/tasks/{id}/water-readings.
type WaterRequest struct {
	Readings []WaterInput `json:"readings"`
}

// ToxinInput is one plate-reader result for a hole.
type ToxinInput struct {
	Hole    string `json:"hole"`
	PSPRaw  string `json:"psp_raw"`
	DSPRaw  string `json:"dsp_raw"`
	Factor  int64  `json:"calibration_factor"`
	Divisor int64  `json:"calibration_divisor"`
}

// ToxinRequest is the body of POST /v1/tasks/{id}/toxin-readings.
type ToxinRequest struct {
	Readings []ToxinInput `json:"readings"`
}

// DeviceCallRequest starts a qPCR or incubator call for a hole.
type DeviceCallRequest struct {
	Hole string `json:"hole"`
	Kind string `json:"kind"` // "qpcr" | "incubator"
}

// DeviceCallbackRequest is a late or normal device response.
type DeviceCallbackRequest struct {
	CallID  int64  `json:"call_id"`
	Payload string `json:"payload"`
}

// RecheckRequest creates the current-generation recheck batch.
type RecheckRequest struct {
	Reason     string   `json:"reason"`
	CageSeals  []string `json:"cage_seals"`
	BlindCodes []string `json:"blind_codes"`
	Timepoints []string `json:"timepoints"`
	Holes      []string `json:"holes"`
}

// RevealRequest authorizes unmasking a blind code.
type RevealRequest struct {
	Code string `json:"code"`
}

// ReviewRequest records one reviewer's conclusion.
type ReviewRequest struct {
	Reviewer string `json:"reviewer"`
	Approved bool   `json:"approved"`
}

// DecisionRequest is the body of a terminal decision (release/isolate/cancel).
type DecisionRequest struct {
	Reason string `json:"reason"`
}

// DispatchRequest advances a releasable task to released.
type DispatchRequest struct{}

// TaskResponse is the public task view.
type TaskResponse struct {
	ID          string   `json:"id"`
	Status      string   `json:"status"`
	Generation  int64    `json:"generation"`
	AreaID      string   `json:"area_id"`
	TideBatch   string   `json:"tide_batch"`
	LockedCount int64    `json:"locked_count"`
	CageSeals   []string `json:"cage_seals"`
	Timepoints  []string `json:"timepoints"`
	Leases      []string `json:"leases"`
	BlindCodes  []string `json:"blind_codes,omitempty"`
}

// DeviceCallResponse is the public view of a device call.
type DeviceCallResponse struct {
	CallID int64  `json:"call_id"`
	Status string `json:"status"`
	Hole   string `json:"hole"`
	Kind   string `json:"kind"`
}

// DecisionResponse is the public view of a terminal decision.
type DecisionResponse struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

// PermitResponse is the public view of a release permit.
type PermitResponse struct {
	PermitNumber string `json:"permit_number"`
	TaskID       string `json:"task_id"`
}

// CoverageResponse reports the vitality coverage state.
type CoverageResponse struct {
	Required int      `json:"required"`
	Recorded int      `json:"recorded"`
	Missing  []string `json:"missing"`
	Closed   bool     `json:"closed"`
}

// EvidenceResponse aggregates the collected evidence.
type EvidenceResponse struct {
	Vitality int `json:"vitality_cells"`
	Water    int `json:"water_readings"`
	Toxin    int `json:"toxin_versions"`
	Pathogen int `json:"pathogen_versions"`
	Reviews  int `json:"reviews"`
}

// AuditResponse is one audit event in the public audit trail.
type AuditResponse struct {
	Seq      int64  `json:"seq"`
	Category string `json:"category"`
	Code     string `json:"code"`
	Detail   string `json:"detail"`
	At       int64  `json:"at"`
}
