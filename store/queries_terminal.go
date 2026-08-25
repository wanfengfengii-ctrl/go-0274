package store

import (
	"context"

	"oyster-purification-release-gate/domain"
)

// InsertTerminal writes the single terminal decision. The primary key on
// task_id guarantees a second decision cannot commit, even under concurrent
// delivery or multi-instance startup.
func (t *Tx) InsertTerminal(ctx context.Context, r TerminalRecord) error {
	_, err := t.ExecContext(ctx, `INSERT INTO terminal_decisions (task_id, decision, reason, winner_op, version)
		VALUES (?, ?, ?, ?, ?)`, r.TaskID, r.Decision, r.Reason, r.WinnerOp, r.Version)
	return err
}

// GetTerminal loads a task's terminal decision, or sql.ErrNoRows.
func (t *Tx) GetTerminal(ctx context.Context, taskID domain.TaskID) (*TerminalRecord, error) {
	row := t.QueryRowContext(ctx, `SELECT task_id, decision, reason, winner_op, version FROM terminal_decisions WHERE task_id = ?`, taskID)
	var r TerminalRecord
	if err := row.Scan(&r.TaskID, &r.Decision, &r.Reason, &r.WinnerOp, &r.Version); err != nil {
		return nil, err
	}
	return &r, nil
}

// InsertPermit writes the unique release permit. task_id is UNIQUE so a task
// can have at most one permit.
func (t *Tx) InsertPermit(ctx context.Context, r PermitRecord) error {
	_, err := t.ExecContext(ctx, `INSERT INTO release_permits (permit_number, task_id, released_at) VALUES (?, ?, ?)`,
		r.PermitNumber, r.TaskID, r.ReleasedAt)
	return err
}

// GetPermit loads a task's release permit, or sql.ErrNoRows.
func (t *Tx) GetPermit(ctx context.Context, taskID domain.TaskID) (*PermitRecord, error) {
	row := t.QueryRowContext(ctx, `SELECT permit_number, task_id, released_at FROM release_permits WHERE task_id = ?`, taskID)
	var r PermitRecord
	if err := row.Scan(&r.PermitNumber, &r.TaskID, &r.ReleasedAt); err != nil {
		return nil, err
	}
	return &r, nil
}
