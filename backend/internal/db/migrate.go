package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationsFS holds the ordered SQL migrations. Files are named
// NNNN_description.sql and applied exactly once, each inside a transaction.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// migration is one parsed SQL file with its numeric version.
type migration struct {
	version int
	name    string
	sql     string
}

// loadMigrations parses and sorts the embedded migration files.
func loadMigrations() ([]migration, error) {
	entries, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	migrations := make([]migration, 0, len(entries))
	for _, name := range entries {
		parts := strings.SplitN(strings.TrimPrefix(name, "migrations/"), "_", 2)
		version, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("migration %q has no numeric prefix", name)
		}
		body, err := migrationsFS.ReadFile(name)
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, migration{version: version, name: name, sql: string(body)})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	return migrations, nil
}

// LatestVersion returns the highest migration version shipped in the binary.
func LatestVersion() (int, error) {
	migrations, err := loadMigrations()
	if err != nil || len(migrations) == 0 {
		return 0, err
	}
	return migrations[len(migrations)-1].version, nil
}

// MigrationsApplied reports the highest applied migration version. It returns
// 0 when the schema_migrations table does not exist yet.
func MigrationsApplied(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'schema_migrations')`,
	).Scan(&exists)
	if err != nil {
		return 0, fmt.Errorf("inspect schema_migrations: %w", err)
	}
	if !exists {
		return 0, nil
	}
	var current int
	if err := pool.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return 0, fmt.Errorf("read schema_migrations: %w", err)
	}
	return current, nil
}

// ApplyMigrations applies every pending migration in order and returns the
// number of migrations applied in this call.
func ApplyMigrations(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	return ApplyMigrationsUpTo(ctx, pool, 0)
}

// ApplyMigrationsUpTo applies pending migrations with version <= maxVersion
// (all when maxVersion is 0). Tests use it to provision an older schema and
// then exercise the upgrade path.
func ApplyMigrationsUpTo(ctx context.Context, pool *pgxpool.Pool, maxVersion int) (int, error) {
	migrations, err := loadMigrations()
	if err != nil {
		return 0, err
	}
	if _, err := pool.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version integer PRIMARY KEY,
			name text NOT NULL,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return 0, fmt.Errorf("create schema_migrations: %w", err)
	}

	applied := 0
	for _, m := range migrations {
		if maxVersion > 0 && m.version > maxVersion {
			break
		}
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, m.version).Scan(&exists); err != nil {
			return applied, fmt.Errorf("read schema_migrations: %w", err)
		}
		if exists {
			continue
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return applied, fmt.Errorf("begin migration transaction: %w", err)
		}
		if _, err := tx.Exec(ctx, m.sql); err != nil {
			_ = tx.Rollback(ctx)
			return applied, fmt.Errorf("apply migration %s: %w", m.name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`, m.version, m.name); err != nil {
			_ = tx.Rollback(ctx)
			return applied, fmt.Errorf("record migration %s: %w", m.name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return applied, fmt.Errorf("commit migration %s: %w", m.name, err)
		}
		applied++
	}
	return applied, nil
}
