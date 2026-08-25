package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Migration is one ordered, versioned schema step.
type Migration struct {
	Version int
	Name    string
	Up      string
}

// Migrations returns the ordered schema migrations in application order. The
// initial migration establishes the core tables required by the documented
// data model (rule snapshots, tasks, cages, samples, blind codes, leases and
// terminal decisions).
func Migrations() []Migration {
	return []Migration{
		{
			Version: 1,
			Name:    "init_core_schema",
			Up: `
CREATE TABLE IF NOT EXISTS rule_snapshots (
    id         TEXT PRIMARY KEY,
    version    INTEGER NOT NULL,
    digest     TEXT    NOT NULL,
    area_id    TEXT    NOT NULL,
    tide_patterns TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS release_tasks (
    id               TEXT PRIMARY KEY,
    status           TEXT    NOT NULL,
    generation       INTEGER NOT NULL,
    area_id          TEXT    NOT NULL,
    tide_batch       TEXT,
    rule_snapshot_id TEXT    NOT NULL,
    locked_count     INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS task_cages (
    task_id TEXT NOT NULL,
    seal    TEXT NOT NULL,
    PRIMARY KEY (task_id, seal)
);

CREATE TABLE IF NOT EXISTS samples (
    id      TEXT PRIMARY KEY,
    task_id TEXT NOT NULL,
    cage_seal TEXT NOT NULL,
    ordinal INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS blind_codes (
    task_id TEXT NOT NULL,
    code    TEXT NOT NULL,
    sample_id TEXT,
    revealed INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_blind_codes_open ON blind_codes(code) WHERE revealed = 0;

CREATE TABLE IF NOT EXISTS resource_leases (
    kind        TEXT NOT NULL,
    resource_key TEXT NOT NULL,
    task_id     TEXT NOT NULL,
    generation  INTEGER NOT NULL,
    acquired_at INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL,
    PRIMARY KEY (kind, resource_key)
);

CREATE TABLE IF NOT EXISTS terminal_decisions (
    task_id    TEXT PRIMARY KEY,
    decision   TEXT NOT NULL,
    reason     TEXT NOT NULL,
    winner_op  TEXT NOT NULL,
    version    INTEGER NOT NULL
);
`,
		},
		{
			Version: 2,
			Name:    "full_release_schema",
			Up: `
ALTER TABLE release_tasks ADD COLUMN rule_digest TEXT NOT NULL DEFAULT '';
ALTER TABLE release_tasks ADD COLUMN uv_lamp_batch TEXT NOT NULL DEFAULT '';
ALTER TABLE release_tasks ADD COLUMN reviewers TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_tasks_open_tide
    ON release_tasks(tide_batch)
    WHERE status NOT IN ('released','isolated','cancelled') AND tide_batch IS NOT NULL AND tide_batch <> '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_cages_seal ON task_cages(seal);

CREATE TABLE IF NOT EXISTS task_timepoints (
    task_id TEXT NOT NULL,
    id      TEXT NOT NULL,
    PRIMARY KEY (task_id, id)
);

CREATE TABLE IF NOT EXISTS task_observation_points (
    task_id TEXT NOT NULL,
    id      TEXT NOT NULL,
    PRIMARY KEY (task_id, id)
);

CREATE TABLE IF NOT EXISTS sample_seals (
    task_id   TEXT NOT NULL,
    cage_seal TEXT NOT NULL,
    location  TEXT NOT NULL,
    PRIMARY KEY (task_id, cage_seal)
);

CREATE TABLE IF NOT EXISTS vitality_cells (
    task_id              TEXT NOT NULL,
    cage_seal            TEXT NOT NULL,
    timepoint_id         TEXT NOT NULL,
    observation_point_id TEXT NOT NULL,
    closed               INTEGER NOT NULL,
    weak_open            INTEGER NOT NULL,
    dead                 INTEGER NOT NULL,
    broken               INTEGER NOT NULL,
    supplement_of        TEXT NOT NULL DEFAULT '',
    version              INTEGER NOT NULL,
    PRIMARY KEY (task_id, cage_seal, timepoint_id, observation_point_id)
);

CREATE TABLE IF NOT EXISTS water_measurements (
    task_id      TEXT NOT NULL,
    cage_seal    TEXT NOT NULL,
    timepoint_id TEXT NOT NULL,
    turbidity    INTEGER NOT NULL,
    salinity     INTEGER NOT NULL,
    temperature  INTEGER NOT NULL,
    chlorine     INTEGER NOT NULL,
    version      INTEGER NOT NULL,
    PRIMARY KEY (task_id, cage_seal, timepoint_id)
);

CREATE TABLE IF NOT EXISTS toxin_evidence_versions (
    task_id      TEXT NOT NULL,
    cage_seal    TEXT NOT NULL,
    timepoint_id TEXT NOT NULL,
    hole         TEXT NOT NULL,
    psp_raw      INTEGER NOT NULL,
    dsp_raw      INTEGER NOT NULL,
    psp          INTEGER NOT NULL,
    dsp          INTEGER NOT NULL,
    version      INTEGER NOT NULL,
    valid        INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (task_id, cage_seal, timepoint_id, hole, version)
);

CREATE TABLE IF NOT EXISTS device_calls (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id    TEXT NOT NULL,
    kind       TEXT NOT NULL,
    hole       TEXT NOT NULL,
    generation INTEGER NOT NULL,
    status     TEXT NOT NULL,
    attempts   INTEGER NOT NULL DEFAULT 0,
    UNIQUE (task_id, kind, hole, generation)
);

CREATE TABLE IF NOT EXISTS device_attempts (
    call_id    INTEGER NOT NULL,
    attempt    INTEGER NOT NULL,
    category   TEXT NOT NULL,
    next_retry INTEGER NOT NULL,
    summary    TEXT NOT NULL,
    PRIMARY KEY (call_id, attempt)
);

CREATE TABLE IF NOT EXISTS pathogen_evidence_versions (
    task_id      TEXT NOT NULL,
    generation   INTEGER NOT NULL,
    hole         TEXT NOT NULL,
    kind         TEXT NOT NULL,
    norovirus_ct INTEGER NOT NULL,
    coliform     INTEGER NOT NULL,
    version      INTEGER NOT NULL,
    valid        INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (task_id, hole, version)
);

CREATE TABLE IF NOT EXISTS recheck_batches (
    task_id    TEXT NOT NULL,
    generation INTEGER NOT NULL,
    scope_json TEXT NOT NULL,
    PRIMARY KEY (task_id, generation)
);

CREATE TABLE IF NOT EXISTS reviews (
    task_id     TEXT NOT NULL,
    generation  INTEGER NOT NULL,
    reviewer    TEXT NOT NULL,
    approved    INTEGER NOT NULL,
    PRIMARY KEY (task_id, generation, reviewer)
);

CREATE TABLE IF NOT EXISTS release_permits (
    permit_number TEXT PRIMARY KEY,
    task_id       TEXT NOT NULL UNIQUE,
    released_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS operation_receipts (
    operation_id TEXT PRIMARY KEY,
    digest       TEXT NOT NULL,
    result       TEXT NOT NULL,
    task_id      TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS person_actions (
    task_id    TEXT NOT NULL,
    person_id  TEXT NOT NULL,
    role       TEXT NOT NULL,
    generation INTEGER NOT NULL,
    PRIMARY KEY (task_id, person_id, role, generation)
);

CREATE TABLE IF NOT EXISTS audit_events (
    seq      INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id  TEXT NOT NULL,
    category TEXT NOT NULL,
    code     TEXT NOT NULL,
    detail   TEXT NOT NULL,
    at       INTEGER NOT NULL
);
`,
		},
	}
}

func ensureMigrationsTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    name    TEXT NOT NULL,
    applied_at INTEGER NOT NULL
);`)
	return err
}

func migrationApplied(ctx context.Context, db *sql.DB, version int) (bool, error) {
	var one int
	err := db.QueryRowContext(ctx,
		`SELECT 1 FROM schema_migrations WHERE version = ?`, version).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func applyMigration(ctx context.Context, db *sql.DB, m Migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, m.Up); err != nil {
		return fmt.Errorf("migration %d (%s): %w", m.Version, m.Name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, 0)`,
		m.Version, m.Name); err != nil {
		return err
	}
	return tx.Commit()
}
