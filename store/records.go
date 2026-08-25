package store

import "oyster-purification-release-gate/domain"

// TaskRecord is the persisted release-task row.
type TaskRecord struct {
	ID             domain.TaskID
	Status         string
	Generation     int64
	AreaID         string
	TideBatch      string
	RuleSnapshotID string
	RuleDigest     string
	LockedCount    int64
	UVLampBatch    string
	Reviewers      string
}

// CageRecord is a persisted cage (seal) bound to a task.
type CageRecord struct {
	TaskID domain.TaskID
	Seal   string
}

// TimepointRecord is a persisted purification timepoint.
type TimepointRecord struct {
	TaskID domain.TaskID
	ID     string
}

// ObservationPointRecord is a persisted observation point.
type ObservationPointRecord struct {
	TaskID domain.TaskID
	ID     string
}

// LeaseRecord is a persisted resource lease.
type LeaseRecord struct {
	Kind        string
	ResourceKey string
	TaskID      domain.TaskID
	Generation  int64
	AcquiredAt  int64
	ExpiresAt   int64
}

// SampleRecord is a persisted triplicate sample.
type SampleRecord struct {
	ID       string
	TaskID   domain.TaskID
	CageSeal string
	Ordinal  int
}

// BlindCodeRecord is a persisted blind code mapping.
type BlindCodeRecord struct {
	TaskID   domain.TaskID
	Code     string
	SampleID string
	Revealed bool
}

// SealRecord is a persisted sample seal.
type SealRecord struct {
	TaskID   domain.TaskID
	CageSeal string
	Location string
}

// VitalityRecord is a persisted vitality cell.
type VitalityRecord struct {
	TaskID             domain.TaskID
	CageSeal           string
	TimepointID        string
	ObservationPointID string
	Closed             int64
	WeakOpen           int64
	Dead               int64
	Broken             int64
	SupplementOf       string // references an original cell, empty if original
	Version            int64
}

// WaterRecord is a persisted water-quality reading.
type WaterRecord struct {
	TaskID      domain.TaskID
	CageSeal    string
	TimepointID string
	Turbidity   int64
	Salinity    int64
	Temperature int64
	Chlorine    int64
	Version     int64
}

// ToxinRecord is a persisted toxin evidence version.
type ToxinRecord struct {
	TaskID      domain.TaskID
	CageSeal    string
	TimepointID string
	Hole        string
	PSPRaw      int64
	DSPRaw      int64
	PSP         int64
	DSP         int64
	Version     int64
	Valid       bool
}

// DeviceCallRecord is a persisted device invocation.
type DeviceCallRecord struct {
	ID         int64
	TaskID     domain.TaskID
	Kind       string
	Hole       string
	Generation int64
	Status     string // pending | success | failed
	Attempts   int
}

// DeviceAttemptRecord is one persisted attempt of a device call.
type DeviceAttemptRecord struct {
	CallID    int64
	Attempt   int
	Category  string
	NextRetry int64
	Summary   string
}

// PathogenRecord is a persisted pathogen evidence version.
type PathogenRecord struct {
	TaskID      domain.TaskID
	Generation  int64
	Hole        string
	Kind        string
	NorovirusCt int64
	Coliform    int64
	Version     int64
	Valid       bool
}

// RecheckRecord is a persisted recheck batch.
type RecheckRecord struct {
	TaskID     domain.TaskID
	Generation int64
	ScopeJSON  string
}

// ReviewRecord is a persisted review conclusion.
type ReviewRecord struct {
	TaskID     domain.TaskID
	Generation int64
	Reviewer   string
	Approved   bool
}

// TerminalRecord is a persisted terminal decision.
type TerminalRecord struct {
	TaskID   domain.TaskID
	Decision string
	Reason   string
	WinnerOp string
	Version  int64
}

// PermitRecord is a persisted release permit.
type PermitRecord struct {
	PermitNumber string
	TaskID       domain.TaskID
	ReleasedAt   int64
}

// ReceiptRecord is a persisted operation receipt.
type ReceiptRecord struct {
	OperationID string
	Digest      string
	Result      string
	TaskID      domain.TaskID
}

// PersonActionRecord is a persisted person action (intake confirmation).
type PersonActionRecord struct {
	TaskID     domain.TaskID
	PersonID   string
	Role       string
	Generation int64
}

// AuditRecord is a persisted, ordered audit event.
type AuditRecord struct {
	Seq      int64
	TaskID   domain.TaskID
	Category string
	Code     string
	Detail   string
	At       int64
}
