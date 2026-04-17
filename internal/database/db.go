package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/acbgbca/xmltvguide/internal/images"
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
	imageCache    *images.Cache
	clock         Clock
	hiddenIDs     []string
	hiddenLCNs    []int
	stripWords    []string

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
//
// populateSQL is optional. When non-empty it is executed immediately after the
// migration's sql is applied, within the same connection, to back-fill any
// derived/computed tables (e.g. FTS indexes, category caches) from data that
// already exists in the database. It runs only once — the first time the
// migration is applied — and is skipped on subsequent startups.
//
// Always include populateSQL for migrations that create derived tables; without
// it those tables remain empty until the next scheduled XMLTV poll (up to 12 h).
var migrations = []struct {
	version     int
	sql         string
	populateSQL string
}{
	{1, `ALTER TABLE channels ADD COLUMN lcn INTEGER`, ""},
	{2, `ALTER TABLE channels ADD COLUMN icon_url TEXT`, ""},
	{3, `CREATE VIRTUAL TABLE IF NOT EXISTS airings_fts USING fts5(
		channel_id, start_time, title, sub_title, description
	)`, `INSERT INTO airings_fts (channel_id, start_time, title, sub_title, description)
		SELECT channel_id, start_time, title, COALESCE(sub_title, ''), COALESCE(description, '')
		FROM airings`},
	{4, `CREATE TABLE IF NOT EXISTS categories (name TEXT PRIMARY KEY)`,
		`INSERT OR IGNORE INTO categories (name)
		SELECT DISTINCT value FROM airings, json_each(airings.categories)
		WHERE value IS NOT NULL AND value != ''`},
}

func applyMigrations(db *sql.DB) error {
	// Migrations run at startup before the server accepts requests;
	// context.Background() is appropriate — migrations are not cancellable.
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, createMigrationsTable); err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}
	for _, m := range migrations {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, m.version).Scan(&count); err != nil {
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
			if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
				m.version, time.Now().UTC().Format(time.RFC3339)); err != nil {
				return fmt.Errorf("recording migration %d: %w", m.version, err)
			}
			continue
		}
		if _, err := db.ExecContext(ctx, m.sql); err != nil {
			return fmt.Errorf("applying migration %d: %w", m.version, err)
		}
		if m.populateSQL != "" {
			if _, err := db.ExecContext(ctx, m.populateSQL); err != nil {
				return fmt.Errorf("populating migration %d: %w", m.version, err)
			}
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
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
	err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, table).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// columnExists reports whether the named column is present in the given table.
func columnExists(db *sql.DB, table, column string) (bool, error) {
	var count int
	err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?", table, column).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// Open opens (or creates) a SQLite database at path and applies the schema.
// imageCache handles icon downloading and caching; pass nil to disable icon caching.
// hiddenIDs and hiddenLCNs specify channels to exclude from all query results;
// pass nil (or empty slices) for no filtering.
// stripWords specifies words/phrases to strip from channel display names at refresh time;
// matching is case-insensitive. Pass nil or empty slice for no stripping.
func Open(path string, retentionDays int, sourceURL string, imageCache *images.Cache, hiddenIDs []string, hiddenLCNs []int, stripWords []string) (*DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	startupCtx := context.Background()
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA temp_store=MEMORY", // scratch image has no /tmp; keep all temp data in RAM
	} {
		if _, err := db.ExecContext(startupCtx, pragma); err != nil {
			if closeErr := db.Close(); closeErr != nil {
				log.Printf("closing database after pragma error: %v", closeErr)
			}
			return nil, fmt.Errorf("setting %s: %w", pragma, err)
		}
	}

	if _, err := db.ExecContext(startupCtx, schema); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			log.Printf("closing database after schema error: %v", closeErr)
		}
		return nil, fmt.Errorf("applying schema: %w", err)
	}

	if err := applyMigrations(db); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			log.Printf("closing database after migration error: %v", closeErr)
		}
		return nil, err
	}

	return &DB{
		db:            db,
		retentionDays: retentionDays,
		sourceURL:     sourceURL,
		imageCache:    imageCache,
		clock:         realClock{},
		hiddenIDs:     hiddenIDs,
		hiddenLCNs:    hiddenLCNs,
		stripWords:    stripWords,
	}, nil
}

// SetClock replaces the DB's clock. Use a fixed clock in tests to make
// time-dependent queries deterministic.
func (d *DB) SetClock(c Clock) {
	d.clock = c
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
func (d *DB) HasData(ctx context.Context) bool {
	var count int
	d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM channels`).Scan(&count) //nolint:errcheck // zero count on error is the safe default
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

// isChannelHidden reports whether the given channel ID or LCN (as a raw string
// from the XMLTV source) should be excluded based on the configured hidden lists.
// lcnStr is the raw LCN string from the XMLTV source; it may be empty or non-numeric.
func (d *DB) isChannelHidden(id, lcnStr string) bool {
	for _, hid := range d.hiddenIDs {
		if hid == id {
			return true
		}
	}
	if len(d.hiddenLCNs) > 0 && lcnStr != "" {
		if n, err := strconv.Atoi(lcnStr); err == nil {
			for _, hlcn := range d.hiddenLCNs {
				if hlcn == n {
					return true
				}
			}
		}
	}
	return false
}

// buildHiddenFilter constructs the SQL fragment and argument slices for deleting
// hidden channels. Returns idArgs, lcnArgs, and a WHERE clause such as:
//
//	id IN (?,?) OR COALESCE(lcn,-1) IN (?,?)
//
// When one list is empty the corresponding clause is omitted.
// Callers must concatenate idArgs + lcnArgs to form the full arg slice.
func buildHiddenFilter(hiddenIDs []string, hiddenLCNs []int) (idArgs []any, lcnArgs []any, sql string) {
	var clauses []string
	if len(hiddenIDs) > 0 {
		ph := make([]string, len(hiddenIDs))
		for i, id := range hiddenIDs {
			ph[i] = "?"
			idArgs = append(idArgs, id)
		}
		clauses = append(clauses, "id IN ("+strings.Join(ph, ",")+")")
	}
	if len(hiddenLCNs) > 0 {
		ph := make([]string, len(hiddenLCNs))
		for i, lcn := range hiddenLCNs {
			ph[i] = "?"
			lcnArgs = append(lcnArgs, lcn)
		}
		clauses = append(clauses, "COALESCE(lcn,-1) IN ("+strings.Join(ph, ",")+")")
	}
	return idArgs, lcnArgs, strings.Join(clauses, " OR ")
}
