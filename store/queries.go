package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"oyster-purification-release-gate/domain"
)

// Tx is the transactional view handed to business commands. It embeds the
// standard library transaction and layers typed data-access methods plus the
// injected logical clock on top.
type Tx struct {
	*sql.Tx
	clock domain.Clock
}

// Now returns the injected logical time.
func (t *Tx) Now() domain.LogicalTime { return t.clock.Now() }

// IsUniqueViolation reports whether err is a SQLite unique-constraint failure,
// which business code translates into a deterministic RESOURCE_BUSY / duplicate
// result.
func IsUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// --- tasks -------------------------------------------------------------

// InsertTask persists a new release-task row.
func (t *Tx) InsertTask(ctx context.Context, r TaskRecord) error {
	_, err := t.ExecContext(ctx, `INSERT INTO release_tasks
		(id, status, generation, area_id, tide_batch, rule_snapshot_id, rule_digest, uv_lamp_batch, locked_count, reviewers)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Status, r.Generation, r.AreaID, r.TideBatch, r.RuleSnapshotID, r.RuleDigest, r.UVLampBatch, r.LockedCount, r.Reviewers)
	return err
}

// GetTask loads one task by id, or sql.ErrNoRows when absent.
func (t *Tx) GetTask(ctx context.Context, id domain.TaskID) (*TaskRecord, error) {
	row := t.QueryRowContext(ctx, `SELECT id, status, generation, area_id,
		COALESCE(tide_batch, ''), rule_snapshot_id, rule_digest, locked_count, uv_lamp_batch, reviewers
		FROM release_tasks WHERE id = ?`, id)
	var r TaskRecord
	err := row.Scan(&r.ID, &r.Status, &r.Generation, &r.AreaID, &r.TideBatch,
		&r.RuleSnapshotID, &r.RuleDigest, &r.LockedCount, &r.UVLampBatch, &r.Reviewers)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// UpdateTaskStatus performs a conditional status advance, returning false when
// the task was not in the expected status (for example after a concurrent
// transition or terminal decision).
func (t *Tx) UpdateTaskStatus(ctx context.Context, id domain.TaskID, from, to string) (bool, error) {
	res, err := t.ExecContext(ctx,
		`UPDATE release_tasks SET status = ? WHERE id = ? AND status = ?`, to, id, from)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// SetTaskGeneration overwrites the task generation (used when a recheck bumps
// the current generation).
func (t *Tx) SetTaskGeneration(ctx context.Context, id domain.TaskID, gen int64) error {
	_, err := t.ExecContext(ctx, `UPDATE release_tasks SET generation = ? WHERE id = ?`, gen, id)
	return err
}

// ListOpenTasks returns tasks that have not reached a terminal state. It backs
// the recovery scanner.
func (t *Tx) ListOpenTasks(ctx context.Context) ([]TaskRecord, error) {
	rows, err := t.QueryContext(ctx, `SELECT id, status, generation, area_id,
		COALESCE(tide_batch,''), rule_snapshot_id, rule_digest, locked_count, uv_lamp_batch, reviewers
		FROM release_tasks WHERE status NOT IN ('released','isolated','cancelled') ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TaskRecord
	for rows.Next() {
		var r TaskRecord
		if err := rows.Scan(&r.ID, &r.Status, &r.Generation, &r.AreaID, &r.TideBatch,
			&r.RuleSnapshotID, &r.RuleDigest, &r.LockedCount, &r.UVLampBatch, &r.Reviewers); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- dimensions --------------------------------------------------------

// InsertCage binds a cage seal to a task.
func (t *Tx) InsertCage(ctx context.Context, r CageRecord) error {
	_, err := t.ExecContext(ctx, `INSERT INTO task_cages (task_id, seal) VALUES (?, ?)`, r.TaskID, r.Seal)
	return err
}

// ListCages returns a task's cages ordered by seal for deterministic output.
func (t *Tx) ListCages(ctx context.Context, taskID domain.TaskID) ([]CageRecord, error) {
	rows, err := t.QueryContext(ctx, `SELECT task_id, seal FROM task_cages WHERE task_id = ? ORDER BY seal`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CageRecord
	for rows.Next() {
		var r CageRecord
		if err := rows.Scan(&r.TaskID, &r.Seal); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertTimepoint records a purification timepoint.
func (t *Tx) InsertTimepoint(ctx context.Context, r TimepointRecord) error {
	_, err := t.ExecContext(ctx, `INSERT INTO task_timepoints (task_id, id) VALUES (?, ?)`, r.TaskID, r.ID)
	return err
}

// ListTimepoints returns timepoints ordered by id.
func (t *Tx) ListTimepoints(ctx context.Context, taskID domain.TaskID) ([]TimepointRecord, error) {
	rows, err := t.QueryContext(ctx, `SELECT task_id, id FROM task_timepoints WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TimepointRecord
	for rows.Next() {
		var r TimepointRecord
		if err := rows.Scan(&r.TaskID, &r.ID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertObservationPoint records an observation point.
func (t *Tx) InsertObservationPoint(ctx context.Context, r ObservationPointRecord) error {
	_, err := t.ExecContext(ctx, `INSERT INTO task_observation_points (task_id, id) VALUES (?, ?)`, r.TaskID, r.ID)
	return err
}

// ListObservationPoints returns observation points ordered by id.
func (t *Tx) ListObservationPoints(ctx context.Context, taskID domain.TaskID) ([]ObservationPointRecord, error) {
	rows, err := t.QueryContext(ctx, `SELECT task_id, id FROM task_observation_points WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ObservationPointRecord
	for rows.Next() {
		var r ObservationPointRecord
		if err := rows.Scan(&r.TaskID, &r.ID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- leases ------------------------------------------------------------

// InsertLease acquires a lease. A unique-constraint failure means the resource
// is already occupied and is surfaced as RESOURCE_BUSY by the caller.
func (t *Tx) InsertLease(ctx context.Context, r LeaseRecord) error {
	_, err := t.ExecContext(ctx, `INSERT INTO resource_leases
		(kind, resource_key, task_id, generation, acquired_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		r.Kind, r.ResourceKey, r.TaskID, r.Generation, r.AcquiredAt, r.ExpiresAt)
	return err
}

// GetLease loads a lease by resource identity, or sql.ErrNoRows.
func (t *Tx) GetLease(ctx context.Context, kind, key string) (*LeaseRecord, error) {
	row := t.QueryRowContext(ctx, `SELECT kind, resource_key, task_id, generation, acquired_at, expires_at
		FROM resource_leases WHERE kind = ? AND resource_key = ?`, kind, key)
	var r LeaseRecord
	err := row.Scan(&r.Kind, &r.ResourceKey, &r.TaskID, &r.Generation, &r.AcquiredAt, &r.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ListLeasesByTask returns all leases held by a task.
func (t *Tx) ListLeasesByTask(ctx context.Context, taskID domain.TaskID) ([]LeaseRecord, error) {
	rows, err := t.QueryContext(ctx, `SELECT kind, resource_key, task_id, generation, acquired_at, expires_at
		FROM resource_leases WHERE task_id = ? ORDER BY kind, resource_key`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LeaseRecord
	for rows.Next() {
		var r LeaseRecord
		if err := rows.Scan(&r.Kind, &r.ResourceKey, &r.TaskID, &r.Generation, &r.AcquiredAt, &r.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteLeasesByTask releases every lease held by a task. This runs only inside
// the terminal transaction.
func (t *Tx) DeleteLeasesByTask(ctx context.Context, taskID domain.TaskID) error {
	_, err := t.ExecContext(ctx, `DELETE FROM resource_leases WHERE task_id = ?`, taskID)
	return err
}

// --- samples, blind codes, seals ---------------------------------------

// InsertSample persists a triplicate sample.
func (t *Tx) InsertSample(ctx context.Context, r SampleRecord) error {
	_, err := t.ExecContext(ctx, `INSERT INTO samples (id, task_id, cage_seal, ordinal) VALUES (?, ?, ?, ?)`,
		r.ID, r.TaskID, r.CageSeal, r.Ordinal)
	return err
}

// ListSamplesByTask returns samples ordered by cage seal then ordinal.
func (t *Tx) ListSamplesByTask(ctx context.Context, taskID domain.TaskID) ([]SampleRecord, error) {
	rows, err := t.QueryContext(ctx, `SELECT id, task_id, cage_seal, ordinal FROM samples WHERE task_id = ? ORDER BY cage_seal, ordinal`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SampleRecord
	for rows.Next() {
		var r SampleRecord
		if err := rows.Scan(&r.ID, &r.TaskID, &r.CageSeal, &r.Ordinal); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertBlindCode binds a blind code to a sample. The partial unique index on
// unrevealed codes enforces global uniqueness across open tasks.
func (t *Tx) InsertBlindCode(ctx context.Context, r BlindCodeRecord) error {
	_, err := t.ExecContext(ctx, `INSERT INTO blind_codes (task_id, code, sample_id, revealed) VALUES (?, ?, ?, ?)`,
		r.TaskID, r.Code, r.SampleID, boolInt(r.Revealed))
	return err
}

// GetBlindCode loads a blind code by its code value.
func (t *Tx) GetBlindCode(ctx context.Context, code string) (*BlindCodeRecord, error) {
	row := t.QueryRowContext(ctx, `SELECT task_id, code, sample_id, revealed FROM blind_codes WHERE code = ?`, code)
	var r BlindCodeRecord
	var rev int
	if err := row.Scan(&r.TaskID, &r.Code, &r.SampleID, &rev); err != nil {
		return nil, err
	}
	r.Revealed = rev != 0
	return &r, nil
}

// RevealBlindCode marks a blind code as revealed (authorized unmasking).
func (t *Tx) RevealBlindCode(ctx context.Context, code string) error {
	_, err := t.ExecContext(ctx, `UPDATE blind_codes SET revealed = 1 WHERE code = ?`, code)
	return err
}

// ListBlindCodesByTask returns the codes bound to a task.
func (t *Tx) ListBlindCodesByTask(ctx context.Context, taskID domain.TaskID) ([]BlindCodeRecord, error) {
	rows, err := t.QueryContext(ctx, `SELECT task_id, code, sample_id, revealed FROM blind_codes WHERE task_id = ? ORDER BY code`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BlindCodeRecord
	for rows.Next() {
		var r BlindCodeRecord
		var rev int
		if err := rows.Scan(&r.TaskID, &r.Code, &r.SampleID, &rev); err != nil {
			return nil, err
		}
		r.Revealed = rev != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertSeal records the physical sealing of a cage.
func (t *Tx) InsertSeal(ctx context.Context, r SealRecord) error {
	_, err := t.ExecContext(ctx, `INSERT INTO sample_seals (task_id, cage_seal, location) VALUES (?, ?, ?)`,
		r.TaskID, r.CageSeal, r.Location)
	return err
}

// ListSealsByTask returns seals ordered by cage seal.
func (t *Tx) ListSealsByTask(ctx context.Context, taskID domain.TaskID) ([]SealRecord, error) {
	rows, err := t.QueryContext(ctx, `SELECT task_id, cage_seal, location FROM sample_seals WHERE task_id = ? ORDER BY cage_seal`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SealRecord
	for rows.Next() {
		var r SealRecord
		if err := rows.Scan(&r.TaskID, &r.CageSeal, &r.Location); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- receipts, person actions, audit -----------------------------------

// InsertReceipt records an operation result for idempotent replay.
func (t *Tx) InsertReceipt(ctx context.Context, r ReceiptRecord) error {
	_, err := t.ExecContext(ctx, `INSERT INTO operation_receipts (operation_id, digest, result, task_id) VALUES (?, ?, ?, ?)`,
		r.OperationID, r.Digest, r.Result, r.TaskID)
	return err
}

// GetReceipt loads a receipt by operation id, or sql.ErrNoRows.
func (t *Tx) GetReceipt(ctx context.Context, opID string) (*ReceiptRecord, error) {
	row := t.QueryRowContext(ctx, `SELECT operation_id, digest, result, task_id FROM operation_receipts WHERE operation_id = ?`, opID)
	var r ReceiptRecord
	if err := row.Scan(&r.OperationID, &r.Digest, &r.Result, &r.TaskID); err != nil {
		return nil, err
	}
	return &r, nil
}

// InsertPersonAction records a person's role action at a generation.
func (t *Tx) InsertPersonAction(ctx context.Context, r PersonActionRecord) error {
	_, err := t.ExecContext(ctx, `INSERT INTO person_actions (task_id, person_id, role, generation) VALUES (?, ?, ?, ?)`,
		r.TaskID, r.PersonID, r.Role, r.Generation)
	return err
}

// ListPersonActions returns the actions for a task and optional role.
func (t *Tx) ListPersonActions(ctx context.Context, taskID domain.TaskID, role string) ([]PersonActionRecord, error) {
	rows, err := t.QueryContext(ctx, `SELECT task_id, person_id, role, generation FROM person_actions WHERE task_id = ? AND role = ? ORDER BY person_id`,
		taskID, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PersonActionRecord
	for rows.Next() {
		var r PersonActionRecord
		if err := rows.Scan(&r.TaskID, &r.PersonID, &r.Role, &r.Generation); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertAudit appends an ordered audit event.
func (t *Tx) InsertAudit(ctx context.Context, r AuditRecord) error {
	_, err := t.ExecContext(ctx, `INSERT INTO audit_events (task_id, category, code, detail, at) VALUES (?, ?, ?, ?, ?)`,
		r.TaskID, r.Category, r.Code, r.Detail, r.At)
	return err
}

// ListAudit returns audit events for a task in sequence order.
func (t *Tx) ListAudit(ctx context.Context, taskID domain.TaskID) ([]AuditRecord, error) {
	rows, err := t.QueryContext(ctx, `SELECT seq, task_id, category, code, detail, at FROM audit_events WHERE task_id = ? ORDER BY seq`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditRecord
	for rows.Next() {
		var r AuditRecord
		if err := rows.Scan(&r.Seq, &r.TaskID, &r.Category, &r.Code, &r.Detail, &r.At); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// boolInt converts a bool to a SQLite integer.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ErrNoRows aliases sql.ErrNoRows for callers that treat absence uniformly.
var ErrNoRows = sql.ErrNoRows

// IsNotFound reports whether err is a no-rows result.
func IsNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }
