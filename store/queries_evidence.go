package store

import (
	"context"

	"oyster-purification-release-gate/domain"
)

// InsertVitality records one vitality cell. The primary key on
// (task, cage, timepoint, observation point) enforces single-writer-per-cell.
func (t *Tx) InsertVitality(ctx context.Context, r VitalityRecord) error {
	_, err := t.ExecContext(ctx, `INSERT INTO vitality_cells
		(task_id, cage_seal, timepoint_id, observation_point_id, closed, weak_open, dead, broken, supplement_of, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.TaskID, r.CageSeal, r.TimepointID, r.ObservationPointID, r.Closed, r.WeakOpen, r.Dead, r.Broken, r.SupplementOf, r.Version)
	return err
}

// GetVitalityCell loads a single cell, or sql.ErrNoRows when not yet recorded.
func (t *Tx) GetVitalityCell(ctx context.Context, taskID domain.TaskID, cage, tp, op string) (*VitalityRecord, error) {
	row := t.QueryRowContext(ctx, `SELECT task_id, cage_seal, timepoint_id, observation_point_id,
		closed, weak_open, dead, broken, supplement_of, version
		FROM vitality_cells WHERE task_id = ? AND cage_seal = ? AND timepoint_id = ? AND observation_point_id = ?`,
		taskID, cage, tp, op)
	var r VitalityRecord
	err := row.Scan(&r.TaskID, &r.CageSeal, &r.TimepointID, &r.ObservationPointID,
		&r.Closed, &r.WeakOpen, &r.Dead, &r.Broken, &r.SupplementOf, &r.Version)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ListVitality returns all cells for a task in deterministic order.
func (t *Tx) ListVitality(ctx context.Context, taskID domain.TaskID) ([]VitalityRecord, error) {
	rows, err := t.QueryContext(ctx, `SELECT task_id, cage_seal, timepoint_id, observation_point_id,
		closed, weak_open, dead, broken, supplement_of, version
		FROM vitality_cells WHERE task_id = ? ORDER BY cage_seal, timepoint_id, observation_point_id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VitalityRecord
	for rows.Next() {
		var r VitalityRecord
		if err := rows.Scan(&r.TaskID, &r.CageSeal, &r.TimepointID, &r.ObservationPointID,
			&r.Closed, &r.WeakOpen, &r.Dead, &r.Broken, &r.SupplementOf, &r.Version); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertWater records one water-quality measurement version.
func (t *Tx) InsertWater(ctx context.Context, r WaterRecord) error {
	_, err := t.ExecContext(ctx, `INSERT INTO water_measurements
		(task_id, cage_seal, timepoint_id, turbidity, salinity, temperature, chlorine, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.TaskID, r.CageSeal, r.TimepointID, r.Turbidity, r.Salinity, r.Temperature, r.Chlorine, r.Version)
	return err
}

// ListWater returns water measurements for a task.
func (t *Tx) ListWater(ctx context.Context, taskID domain.TaskID) ([]WaterRecord, error) {
	rows, err := t.QueryContext(ctx, `SELECT task_id, cage_seal, timepoint_id, turbidity, salinity, temperature, chlorine, version
		FROM water_measurements WHERE task_id = ? ORDER BY cage_seal, timepoint_id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WaterRecord
	for rows.Next() {
		var r WaterRecord
		if err := rows.Scan(&r.TaskID, &r.CageSeal, &r.TimepointID, &r.Turbidity, &r.Salinity, &r.Temperature, &r.Chlorine, &r.Version); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertToxin appends an immutable toxin evidence version.
func (t *Tx) InsertToxin(ctx context.Context, r ToxinRecord) error {
	_, err := t.ExecContext(ctx, `INSERT INTO toxin_evidence_versions
		(task_id, cage_seal, timepoint_id, hole, psp_raw, dsp_raw, psp, dsp, version, valid)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.TaskID, r.CageSeal, r.TimepointID, r.Hole, r.PSPRaw, r.DSPRaw, r.PSP, r.DSP, r.Version, boolInt(r.Valid))
	return err
}

// ListToxin returns toxin evidence versions for a task.
func (t *Tx) ListToxin(ctx context.Context, taskID domain.TaskID) ([]ToxinRecord, error) {
	rows, err := t.QueryContext(ctx, `SELECT task_id, cage_seal, timepoint_id, hole, psp_raw, dsp_raw, psp, dsp, version, valid
		FROM toxin_evidence_versions WHERE task_id = ? ORDER BY cage_seal, timepoint_id, hole, version`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ToxinRecord
	for rows.Next() {
		var r ToxinRecord
		var valid int
		if err := rows.Scan(&r.TaskID, &r.CageSeal, &r.TimepointID, &r.Hole, &r.PSPRaw, &r.DSPRaw, &r.PSP, &r.DSP, &r.Version, &valid); err != nil {
			return nil, err
		}
		r.Valid = valid != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- device calls ------------------------------------------------------

// InsertDeviceCall persists a pending device invocation and returns its id.
func (t *Tx) InsertDeviceCall(ctx context.Context, r DeviceCallRecord) (int64, error) {
	res, err := t.ExecContext(ctx, `INSERT INTO device_calls (task_id, kind, hole, generation, status, attempts)
		VALUES (?, ?, ?, ?, ?, ?)`, r.TaskID, r.Kind, r.Hole, r.Generation, r.Status, r.Attempts)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateDeviceCall mutates a device call's status and attempt counter.
func (t *Tx) UpdateDeviceCall(ctx context.Context, id int64, status string, attempts int) error {
	_, err := t.ExecContext(ctx, `UPDATE device_calls SET status = ?, attempts = ? WHERE id = ?`, status, attempts, id)
	return err
}

// GetDeviceCall loads a device call by identity, or sql.ErrNoRows.
func (t *Tx) GetDeviceCall(ctx context.Context, taskID domain.TaskID, kind, hole string, generation int64) (*DeviceCallRecord, error) {
	row := t.QueryRowContext(ctx, `SELECT id, task_id, kind, hole, generation, status, attempts
		FROM device_calls WHERE task_id = ? AND kind = ? AND hole = ? AND generation = ?`, taskID, kind, hole, generation)
	var r DeviceCallRecord
	err := row.Scan(&r.ID, &r.TaskID, &r.Kind, &r.Hole, &r.Generation, &r.Status, &r.Attempts)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ListPendingDeviceCalls returns device calls that are persisted but not yet
// complete, so the restart recovery scanner can re-drive them. It covers two
// crash outcomes:
//   - a call left "pending" by a crash between persisting the call and its
//     first device attempt: it never ran, has no attempts, and is driven
//     immediately, and
//   - a "failed" call whose most recent retry time — scheduled by its last
//     failed attempt — has arrived.
//
// Once a call reaches "success" it is terminal and excluded, so recovery
// resumes a crashed call exactly once without duplicating evidence.
func (t *Tx) ListPendingDeviceCalls(ctx context.Context, now int64) ([]DeviceCallRecord, error) {
	rows, err := t.QueryContext(ctx, `
		SELECT dc.id, dc.task_id, dc.kind, dc.hole, dc.generation, dc.status, dc.attempts
		FROM device_calls dc
		WHERE dc.status = 'pending'
		   OR (dc.status = 'failed'
		       AND COALESCE((
		           SELECT MAX(da.next_retry) FROM device_attempts da WHERE da.call_id = dc.id
		       ), 0) <= ?)
		ORDER BY dc.id`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeviceCallRecord
	for rows.Next() {
		var r DeviceCallRecord
		if err := rows.Scan(&r.ID, &r.TaskID, &r.Kind, &r.Hole, &r.Generation, &r.Status, &r.Attempts); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertDeviceAttempt appends one attempt (success or failure) of a device call.
func (t *Tx) InsertDeviceAttempt(ctx context.Context, r DeviceAttemptRecord) error {
	_, err := t.ExecContext(ctx, `INSERT INTO device_attempts (call_id, attempt, category, next_retry, summary) VALUES (?, ?, ?, ?, ?)`,
		r.CallID, r.Attempt, r.Category, r.NextRetry, r.Summary)
	return err
}

// --- pathogen ----------------------------------------------------------

// InsertPathogen appends an immutable pathogen evidence version.
func (t *Tx) InsertPathogen(ctx context.Context, r PathogenRecord) error {
	_, err := t.ExecContext(ctx, `INSERT INTO pathogen_evidence_versions
		(task_id, generation, hole, kind, norovirus_ct, coliform, version, valid)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.TaskID, r.Generation, r.Hole, r.Kind, r.NorovirusCt, r.Coliform, r.Version, boolInt(r.Valid))
	return err
}

// ListPathogen returns pathogen evidence versions for a task.
func (t *Tx) ListPathogen(ctx context.Context, taskID domain.TaskID) ([]PathogenRecord, error) {
	rows, err := t.QueryContext(ctx, `SELECT task_id, generation, hole, kind, norovirus_ct, coliform, version, valid
		FROM pathogen_evidence_versions WHERE task_id = ? ORDER BY hole, version`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PathogenRecord
	for rows.Next() {
		var r PathogenRecord
		var valid int
		if err := rows.Scan(&r.TaskID, &r.Generation, &r.Hole, &r.Kind, &r.NorovirusCt, &r.Coliform, &r.Version, &valid); err != nil {
			return nil, err
		}
		r.Valid = valid != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- recheck -----------------------------------------------------------

// InsertRecheck records the single current-generation recheck batch. The
// primary key on (task, generation) enforces one-batch-per-generation.
func (t *Tx) InsertRecheck(ctx context.Context, r RecheckRecord) error {
	_, err := t.ExecContext(ctx, `INSERT INTO recheck_batches (task_id, generation, scope_json) VALUES (?, ?, ?)`,
		r.TaskID, r.Generation, r.ScopeJSON)
	return err
}

// GetRecheck loads a recheck batch, or sql.ErrNoRows.
func (t *Tx) GetRecheck(ctx context.Context, taskID domain.TaskID, generation int64) (*RecheckRecord, error) {
	row := t.QueryRowContext(ctx, `SELECT task_id, generation, scope_json FROM recheck_batches WHERE task_id = ? AND generation = ?`,
		taskID, generation)
	var r RecheckRecord
	if err := row.Scan(&r.TaskID, &r.Generation, &r.ScopeJSON); err != nil {
		return nil, err
	}
	return &r, nil
}

// --- reviews -----------------------------------------------------------

// InsertReview records one review conclusion.
func (t *Tx) InsertReview(ctx context.Context, r ReviewRecord) error {
	_, err := t.ExecContext(ctx, `INSERT INTO reviews (task_id, generation, reviewer, approved) VALUES (?, ?, ?, ?)`,
		r.TaskID, r.Generation, r.Reviewer, boolInt(r.Approved))
	return err
}

// ListReviews returns reviews for a task generation ordered by reviewer.
func (t *Tx) ListReviews(ctx context.Context, taskID domain.TaskID, generation int64) ([]ReviewRecord, error) {
	rows, err := t.QueryContext(ctx, `SELECT task_id, generation, reviewer, approved FROM reviews WHERE task_id = ? AND generation = ? ORDER BY reviewer`,
		taskID, generation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReviewRecord
	for rows.Next() {
		var r ReviewRecord
		var approved int
		if err := rows.Scan(&r.TaskID, &r.Generation, &r.Reviewer, &approved); err != nil {
			return nil, err
		}
		r.Approved = approved != 0
		out = append(out, r)
	}
	return out, rows.Err()
}
