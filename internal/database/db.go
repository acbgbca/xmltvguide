package database

import (
	"database/sql"
	"fmt"
	"net/http"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/acbgbca/xmltvguide/internal/model"
)

const schema = `
CREATE TABLE IF NOT EXISTS channels (
	id           TEXT    PRIMARY KEY,
	display_name TEXT    NOT NULL,
	icon         TEXT,
	sort_order   INTEGER NOT NULL,
	lcn          INTEGER
);

CREATE TABLE IF NOT EXISTS airings (
	channel_id          TEXT    NOT NULL REFERENCES channels(id),
	start_time          TEXT    NOT NULL,
	stop_time           TEXT    NOT NULL,
	title               TEXT    NOT NULL DEFAULT '',
	sub_title           TEXT,
	description         TEXT,
	categories          TEXT,
	episode_num         TEXT,
	episode_num_display TEXT,
	prog_id             TEXT,
	star_rating         TEXT,
	content_rating      TEXT,
	year                TEXT,
	icon                TEXT,
	country             TEXT,
	is_repeat           INTEGER NOT NULL DEFAULT 0,
	is_premiere         INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (channel_id, start_time)
);

CREATE INDEX IF NOT EXISTS idx_airings_window ON airings(start_time, stop_time);
CREATE INDEX IF NOT EXISTS idx_airings_stop   ON airings(stop_time);
`

// DB wraps a SQLite database connection.
type DB struct {
	db            *sql.DB
	retentionDays int
	sourceURL     string
	imageCacheDir string
	httpClient    *http.Client

	// lastRefresh and nextRefresh are kept in memory — they reflect the current
	// process's poll cycle and reset on restart, which is intentional.
	mu          sync.RWMutex
	lastRefresh time.Time
	nextRefresh time.Time
}

const createMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at TEXT    NOT NULL
);`

// migrations is the ordered list of schema changes applied after the base schema.
// Append new entries; never modify or remove existing ones.
var migrations = []struct {
	version int
	sql     string
}{
	{1, `ALTER TABLE channels ADD COLUMN lcn INTEGER`},
	{2, `ALTER TABLE channels ADD COLUMN icon_url TEXT`},
	{3, `CREATE VIRTUAL TABLE IF NOT EXISTS airings_fts USING fts5(
		channel_id, start_time, title, sub_title, description
	)`},
	{4, `CREATE TABLE IF NOT EXISTS categories (name TEXT PRIMARY KEY)`},
}

func applyMigrations(db *sql.DB) error {
	if _, err := db.Exec(createMigrationsTable); err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}
	for _, m := range migrations {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, m.version).Scan(&count); err != nil {
			return fmt.Errorf("checking migration %d: %w", m.version, err)
		}
		if count > 0 {
			continue
		}
		// Before running, check whether the effect is already present.
		// This handles databases that were created with the column already in the
		// base schema, or that ran the old silent ALTER TABLE approach.
		if already, err := migrationAlreadyApplied(db, m.version); err != nil {
			return fmt.Errorf("pre-checking migration %d: %w", m.version, err)
		} else if already {
			if _, err := db.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
				m.version, time.Now().UTC().Format(time.RFC3339)); err != nil {
				return fmt.Errorf("recording migration %d: %w", m.version, err)
			}
			continue
		}
		if _, err := db.Exec(m.sql); err != nil {
			return fmt.Errorf("applying migration %d: %w", m.version, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			m.version, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return fmt.Errorf("recording migration %d: %w", m.version, err)
		}
	}
	return nil
}

// migrationAlreadyApplied reports whether migration n's schema change is
// detectably present, even when it was applied outside the version table
// (e.g. by the old silent ALTER TABLE approach, or from the base schema).
func migrationAlreadyApplied(db *sql.DB, version int) (bool, error) {
	switch version {
	case 1:
		return columnExists(db, "channels", "lcn")
	case 2:
		return columnExists(db, "channels", "icon_url")
	case 3:
		return tableExists(db, "airings_fts")
	case 4:
		return tableExists(db, "categories")
	}
	return false, nil
}

// tableExists reports whether a table (or virtual table) exists in the database.
func tableExists(db *sql.DB, table string) (bool, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, table).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// columnExists reports whether the named column is present in the given table.
func columnExists(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, colType string
		var dfltValue sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// Open opens (or creates) a SQLite database at path and applies the schema.
// imageCacheDir is the directory used to store downloaded channel icons.
// httpClient is used for icon downloads; pass nil to disable icon caching.
func Open(path string, retentionDays int, sourceURL, imageCacheDir string, httpClient *http.Client) (*DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA temp_store=MEMORY", // scratch image has no /tmp; keep all temp data in RAM
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("setting %s: %w", pragma, err)
		}
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
	}

	if err := applyMigrations(db); err != nil {
		db.Close()
		return nil, err
	}

	return &DB{
		db:            db,
		retentionDays: retentionDays,
		sourceURL:     sourceURL,
		imageCacheDir: imageCacheDir,
		httpClient:    httpClient,
	}, nil
}

// GetStatus returns the current refresh metadata.
func (d *DB) GetStatus() model.Status {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return model.Status{
		LastRefresh: d.lastRefresh,
		NextRefresh: d.nextRefresh,
		SourceURL:   d.sourceURL,
	}
}

// HasData reports whether the database contains any channels.
func (d *DB) HasData() bool {
	var count int
	d.db.QueryRow(`SELECT COUNT(*) FROM channels`).Scan(&count) //nolint:errcheck — zero count on error is the safe default
	return count > 0
}

// SetNextRefresh updates the in-memory next refresh time without performing a fetch.
// Call this when the startup fetch is skipped so that /api/status reflects the correct schedule.
func (d *DB) SetNextRefresh(t time.Time) {
	d.mu.Lock()
	d.nextRefresh = t
	d.mu.Unlock()
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.db.Close()
}
