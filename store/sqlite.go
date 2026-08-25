package store

import (
	"context"
	"database/sql"
	"fmt"

	"oyster-purification-release-gate/domain"
)

// Store is the concrete, SQLite-backed persistence boundary. It owns the
// database handle, the injected logical clock and every typed data-access
// method. Business commands run inside a single transaction via Tx.
type Store struct {
	db    *sql.DB
	clock domain.Clock
}

// Open opens (or creates) the SQLite database at dsn, applies pending
// migrations and verifies connectivity. dsn is passed straight to the driver;
// use ":memory:" for a throwaway store or a file path for restart recovery.
func Open(ctx context.Context, dsn string, clock domain.Clock) (*Store, error) {
	if clock == nil {
		clock = domain.FixedClock(0)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// A single writer plus busy timeout keeps concurrent transactions from
	// failing spuriously while the unique constraints arbitrate real conflicts.
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := Migrate(ctx, db); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db, clock: clock}, nil
}

// DB exposes the underlying handle for health checks and recovery scans.
func (s *Store) DB() *sql.DB { return s.db }

// Clock returns the injected logical clock.
func (s *Store) Clock() domain.Clock { return s.clock }

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// Ping verifies database reachability.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// Tx runs fn inside exactly one transaction. If fn returns an error the
// transaction rolls back with no partial business trace; otherwise it commits.
// This is the single transaction boundary every business command goes through.
func (s *Store) Tx(ctx context.Context, fn func(*Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(&Tx{Tx: tx, clock: s.clock}); err != nil {
		return err
	}
	return tx.Commit()
}
