package postgres

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const migrationNamespace = "notification-service"

// RunMigrations applies pending *.sql files in lexical order.
func RunMigrations(db *sql.DB, migrationsDir string) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS public.schema_migrations (
			filename   TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("migrations: create tracking table: %w", err)
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("migrations: dir %q does not exist", migrationsDir)
		}
		return fmt.Errorf("migrations: read dir %q: %w", migrationsDir, err)
	}

	files := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)

	for _, filename := range files {
		migrationKey := migrationNamespace + "/" + filename

		var alreadyApplied int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM public.schema_migrations WHERE filename = $1`,
			migrationKey,
		).Scan(&alreadyApplied); err != nil {
			return fmt.Errorf("migrations: check %s: %w", filename, err)
		}
		if alreadyApplied > 0 {
			continue
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
			migrationKey,
		); err != nil {
			return fmt.Errorf("migrations: record %s: %w", filename, err)
		}
	}

	return nil
}
