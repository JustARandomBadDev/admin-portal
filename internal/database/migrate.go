package database

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/JustARandomBadDev/captive-portal-admin/migrations"
	"github.com/jackc/pgx/v5"
)

type migration struct {
	version string
	name    string
	number  uint64
	sql     string
}

var migrationName = regexp.MustCompile(`^([0-9]+)_[A-Za-z0-9_]+\.sql$`)

func discoverMigrations(files fs.FS) ([]migration, error) {
	names, err := fs.Glob(files, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("discover migrations: %w", err)
	}
	result := make([]migration, 0, len(names))
	seen := make(map[uint64]string)
	for _, name := range names {
		match := migrationName.FindStringSubmatch(name)
		if match == nil {
			return nil, fmt.Errorf("invalid migration filename %q", name)
		}
		number, err := strconv.ParseUint(match[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid migration version in %q: %w", name, err)
		}
		if previous, ok := seen[number]; ok {
			return nil, fmt.Errorf("duplicate migration version: %q and %q", previous, name)
		}
		seen[number] = name
		content, err := fs.ReadFile(files, name)
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", name, err)
		}
		result = append(result, migration{match[1], name, number, string(content)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].number < result[j].number })
	return result, nil
}

// Migrate runs only when explicitly requested by adminctl, never at HTTP startup.
func (h *Handle) Migrate(ctx context.Context, output io.Writer) (err error) {
	items, err := discoverMigrations(migrations.Files)
	if err != nil {
		return err
	}
	conn, err := h.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()
	// A session lock must stay on this dedicated connection, including all transactions.
	if _, err = conn.Exec(ctx, "SELECT pg_advisory_lock(hashtext('admin-portal-migrations'))"); err != nil {
		_ = conn.Conn().Close(context.Background())
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, unlockErr := conn.Exec(cleanupCtx, "SELECT pg_advisory_unlock(hashtext('admin-portal-migrations'))"); unlockErr != nil {
			// Never return a session with an outstanding lock to the pool.
			_ = conn.Conn().Close(cleanupCtx)
			if err == nil {
				err = fmt.Errorf("release migration lock: %w", unlockErr)
			}
		}
	}()
	if _, err = conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`); err != nil {
		return fmt.Errorf("create migration history: %w", err)
	}
	fmt.Fprintln(output, "Database migrations:")
	for _, item := range items {
		var applied bool
		if err = conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)", item.version).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", item.name, err)
		}
		if applied {
			fmt.Fprintf(output, "- %s: already applied\n", item.name)
			continue
		}
		fmt.Fprintf(output, "- %s: applying\n", item.name)
		err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			// Simple protocol accepts the complete multi-statement SQL without splitting it.
			if _, err := tx.Exec(ctx, item.sql, pgx.QueryExecModeSimpleProtocol); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "INSERT INTO schema_migrations(version, name) VALUES ($1, $2)", item.version, item.name)
			return err
		})
		if err != nil {
			return fmt.Errorf("apply migration %s: %w", item.name, err)
		}
		fmt.Fprintf(output, "- %s: applied\n", item.name)
	}
	fmt.Fprintln(output, "Database schema is up to date")
	return nil
}
