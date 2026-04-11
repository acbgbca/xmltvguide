package database_test

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/acbgbca/xmltvguide/internal/database"
	"github.com/acbgbca/xmltvguide/internal/images"
)

// preMigrationSchema is the table structure of a database that already has
// all column-level migrations (1 and 2) but lacks the derived tables created
// by migrations 3 (airings_fts) and 4 (categories). This simulates an
// existing database that needs those migrations applied.
const preMigrationSchema = `
CREATE TABLE channels (
	id           TEXT    PRIMARY KEY,
	display_name TEXT    NOT NULL,
	icon         TEXT,
	sort_order   INTEGER NOT NULL,
	lcn          INTEGER,
	icon_url     TEXT
);
CREATE TABLE airings (
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

// createPreMigrationDB creates a SQLite database at the given path with the
// pre-migration schema (no airings_fts, no categories) and inserts the
// provided channel and airing rows. This simulates an existing production
// database prior to migrations 3 and 4 being applied.
func createPreMigrationDB(t *testing.T, path string, channelID string, categories string) {
	t.Helper()
	rawDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("createPreMigrationDB: open: %v", err)
	}
	defer rawDB.Close()

	if _, err := rawDB.Exec(preMigrationSchema); err != nil {
		t.Fatalf("createPreMigrationDB: schema: %v", err)
	}
	if _, err := rawDB.Exec(
		`INSERT INTO channels (id, display_name, sort_order) VALUES (?, ?, 1)`,
		channelID, "Test Channel",
	); err != nil {
		t.Fatalf("createPreMigrationDB: insert channel: %v", err)
	}
	if _, err := rawDB.Exec(`
		INSERT INTO airings (channel_id, start_time, stop_time, title, categories)
		VALUES (?, '2025-01-01T10:00:00Z', '2025-01-01T11:00:00Z', 'Test Show', ?)`,
		channelID, categories,
	); err != nil {
		t.Fatalf("createPreMigrationDB: insert airing: %v", err)
	}
}

// openDBAt opens a database.DB at the given path using a failing HTTP client.
func openDBAt(t *testing.T, path string) *database.DB {
	t.Helper()
	dir := filepath.Dir(path)
	client := &http.Client{Transport: &failingTransport{}}
	cache := images.NewCache(client, filepath.Join(dir, "images"))
	db, err := database.Open(path, 7, "http://test", cache, nil, nil, nil)
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestMigration_PopulateSQL_RunsOnNewMigration verifies that when migrations 3
// and 4 are applied to a database that already contains airings, the
// populateSQL runs immediately and populates airings_fts and categories without
// requiring an XMLTV network fetch.
func TestMigration_PopulateSQL_RunsOnNewMigration(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	createPreMigrationDB(t, dbPath, "ch1", `["News","Sport"]`)

	db := openDBAt(t, dbPath)

	// categories should be populated from the existing airing.
	cats, err := db.GetCategories()
	if err != nil {
		t.Fatalf("GetCategories: %v", err)
	}
	if len(cats) == 0 {
		t.Fatal("expected categories to be populated by populateSQL, got none")
	}
	wantCats := map[string]bool{"News": true, "Sport": true}
	for _, c := range cats {
		if !wantCats[c] {
			t.Errorf("unexpected category %q", c)
		}
		delete(wantCats, c)
	}
	for c := range wantCats {
		t.Errorf("missing expected category %q", c)
	}

	// airings_fts should be populated — searching with includePast=true should find the airing.
	results, err := db.SearchAdvanced("Test Show", nil, true, true, false)
	if err != nil {
		t.Fatalf("SearchAdvanced: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected airings_fts to be populated by populateSQL, got no search results")
	}
}

// TestMigration_PopulateSQL_SkipsAlreadyApplied verifies that populateSQL does
// NOT re-run when a migration is already recorded in schema_migrations. Opening
// the database a second time after migrations have been applied should not
// re-populate derived tables from data inserted since the first open.
func TestMigration_PopulateSQL_SkipsAlreadyApplied(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// First open: creates a fresh DB, applies all migrations (tables are empty).
	// Close immediately after — we don't use t.Cleanup here so we can control timing.
	{
		client := &http.Client{Transport: &failingTransport{}}
		cache := images.NewCache(client, filepath.Join(dir, "images"))
		db1, err := database.Open(dbPath, 7, "http://test", cache, nil, nil, nil)
		if err != nil {
			t.Fatalf("database.Open (first): %v", err)
		}
		db1.Close()
	}

	// Now insert data directly — bypassing Refresh() — to simulate data added
	// after migrations were first applied.
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if _, err := rawDB.Exec(
		`INSERT INTO channels (id, display_name, sort_order) VALUES ('ch1', 'Test Channel', 1)`,
	); err != nil {
		rawDB.Close()
		t.Fatalf("insert channel: %v", err)
	}
	if _, err := rawDB.Exec(`
		INSERT INTO airings (channel_id, start_time, stop_time, title, categories)
		VALUES ('ch1', '2025-01-01T10:00:00Z', '2025-01-01T11:00:00Z', 'Test Show', '["Sports"]')`,
	); err != nil {
		rawDB.Close()
		t.Fatalf("insert airing: %v", err)
	}
	rawDB.Close()

	// Second open: migrations are already recorded — populateSQL should NOT run.
	cache := images.NewCache(&http.Client{Transport: &failingTransport{}}, filepath.Join(dir, "images2"))
	db2, err := database.Open(dbPath, 7, "http://test", cache, nil, nil, nil)
	if err != nil {
		t.Fatalf("database.Open (second): %v", err)
	}
	defer db2.Close()

	cats, err := db2.GetCategories()
	if err != nil {
		t.Fatalf("GetCategories: %v", err)
	}
	if len(cats) != 0 {
		t.Errorf("expected categories to be empty (populateSQL should not re-run), got %v", cats)
	}

	results, err := db2.SearchAdvanced("Test Show", nil, true, true, false)
	if err != nil {
		t.Fatalf("SearchAdvanced: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected FTS to be empty (populateSQL should not re-run), got %d results", len(results))
	}
}
