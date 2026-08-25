// Package store provides the persistence boundary: schema migrations,
// transaction boundaries, conditional writes, unique constraints and recovery
// scanning. It is the only package that talks to the relational database. The
// concrete SQLite-backed implementation lives in sqlite.go and queries.go.
package store

import (
	"context"
	"database/sql"
)

// Migrate applies all ordered migrations that have not yet run. Each migration
// runs in its own transaction and is recorded by version.
func Migrate(ctx context.Context, db *sql.DB) error {
	if err := ensureMigrationsTable(ctx, db); err != nil {
		return err
	}
	for _, m := range Migrations() {
		applied, err := migrationApplied(ctx, db, m.Version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := applyMigration(ctx, db, m); err != nil {
			return err
		}
	}
	return nil
}
