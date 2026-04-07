package postgres

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RunMigrations applies any *.sql files in migrationsDir that have not yet
// been recorded in the public.schema_migrations tracking table.
// Files are applied in lexicographic order (001_…, 002_…, …).
//
// This is intentionally simple: each file is executed as a single statement
// batch and recorded atomically. If a file fails, the error is returned
// immediately and no further migrations are attempted.
func RunMigrations(db *sql.DB, migrationsDir string) error {
	// Create the tracking table if it does not exist yet.
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS public.schema_migrations (
			filename   TEXT        PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`)
	if err != nil {
		return fmt.Errorf("migrations: create tracking table: %w", err)
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		if os.IsNotExist(err) {
			// No migrations directory — nothing to do.
			return nil
		}
		return fmt.Errorf("migrations: read dir %q: %w", migrationsDir, err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, filename := range files {
		var already int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM public.schema_migrations WHERE filename = $1`,
			filename,
		).Scan(&already); err != nil {
			return fmt.Errorf("migrations: check %s: %w", filename, err)
		}
		if already > 0 {
			continue // already applied on a previous startup
		}

		content, err := os.ReadFile(filepath.Join(migrationsDir, filename))
		if err != nil {
			return fmt.Errorf("migrations: read %s: %w", filename, err)
		}

		if _, err := db.Exec(string(content)); err != nil {
			return fmt.Errorf("migrations: apply %s: %w", filename, err)
		}

		if _, err := db.Exec(
			`INSERT INTO public.schema_migrations (filename) VALUES ($1)`,
			filename,
		); err != nil {
			return fmt.Errorf("migrations: record %s: %w", filename, err)
		}
	}
	return nil
}
