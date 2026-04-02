package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/acbgbca/xmltvguide/internal/xmltv"
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

// Channel holds display data for a TV channel.
type Channel struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Icon        string `json:"icon,omitempty"`
	LCN         *int   `json:"lcn,omitempty"`
}

// Airing holds all data for a single broadcast slot.
// JSON field names for start/stop are kept as "start"/"stop" for frontend compatibility.
type Airing struct {
	ChannelID         string    `json:"channelId"`
	Start             time.Time `json:"start"`
	Stop              time.Time `json:"stop"`
	Title             string    `json:"title"`
	SubTitle          string    `json:"subTitle,omitempty"`
	Description       string    `json:"description,omitempty"`
	Categories        []string  `json:"categories,omitempty"`
	EpisodeNum        string    `json:"episodeNum,omitempty"`
	EpisodeNumDisplay string    `json:"episodeNumDisplay,omitempty"`
	ProgID            string    `json:"progId,omitempty"`
	StarRating        string    `json:"starRating,omitempty"`
	ContentRating     string    `json:"contentRating,omitempty"`
	Year              string    `json:"year,omitempty"`
	Icon              string    `json:"icon,omitempty"`
	Country           string    `json:"country,omitempty"`
	IsRepeat          bool      `json:"isRepeat"`
	IsPremiere        bool      `json:"isPremiere"`
}

// Status holds metadata about the last data refresh.
type Status struct {
	LastRefresh time.Time `json:"lastRefresh"`
	NextRefresh time.Time `json:"nextRefresh"`
	SourceURL   string    `json:"sourceUrl"`
}

// SearchResult extends Airing with additional fields needed by the search API.
type SearchResult struct {
	Airing
	ChannelName string  `json:"channelName"`
	Rank        float64 `json:"-"`
}

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

// Refresh atomically loads a parsed XMLTV document into the database,
// then prunes airings older than the configured retention period.
// Existing airings with the same (channel_id, start_time) are replaced
// with fresh data; historical airings not present in the file are left intact.
// Icons are downloaded when the URL has changed or the local file is missing.
func (d *DB) Refresh(ctx context.Context, tv *xmltv.TV, nextRefresh time.Time) error {
	// Phase 1: Query current channel icon state so we can detect URL changes.
	type iconState struct{ localPath, iconURL string }
	currentIcons := map[string]iconState{}
	if rows, err := d.db.QueryContext(ctx,
		`SELECT id, COALESCE(icon, ''), COALESCE(icon_url, '') FROM channels`,
	); err == nil {
		defer rows.Close()
		for rows.Next() {
			var id, localPath, iconURL string
			if rows.Scan(&id, &localPath, &iconURL) == nil {
				currentIcons[id] = iconState{localPath, iconURL}
			}
		}
	}

	// Phase 2: Resolve icons — download when URL changed or file is absent.
	type resolvedIcon struct{ localPath, iconURL string }
	resolved := map[string]resolvedIcon{}

	for _, ch := range tv.Channels {
		if len(ch.Icons) == 0 {
			continue
		}
		incomingURL := ch.Icons[0].Src
		existing := currentIcons[ch.ID]

		// Skip download if URL is unchanged and the cached file still exists.
		if existing.iconURL == incomingURL && existing.localPath != "" {
			if _, err := os.Stat(existing.localPath); err == nil {
				resolved[ch.ID] = resolvedIcon{existing.localPath, incomingURL}
				continue
			}
		}

		// Attempt download only when the cache is configured.
		if d.imageCacheDir != "" && d.httpClient != nil {
			localPath, err := d.downloadAndSaveIcon(ctx, ch.ID, incomingURL)
			if err != nil {
				log.Printf("warning: failed to cache icon for channel %s: %v", ch.ID, err)
				// Store icon_url even if download failed so the handler can
				// re-download on demand.
				resolved[ch.ID] = resolvedIcon{"", incomingURL}
				continue
			}
			resolved[ch.ID] = resolvedIcon{localPath, incomingURL}
			continue
		}
		// Cache not configured: store the URL for future use, no local file.
		resolved[ch.ID] = resolvedIcon{"", incomingURL}
	}

	// Phase 3: Write everything to the database in a single transaction.
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	chStmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO channels (id, display_name, icon, sort_order, lcn, icon_url)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("preparing channel upsert: %w", err)
	}
	defer chStmt.Close()

	for i, ch := range tv.Channels {
		name := ch.ID
		if len(ch.DisplayNames) > 0 && ch.DisplayNames[0].Value != "" {
			name = ch.DisplayNames[0].Value
		}
		var icon, iconURL any
		if ri, ok := resolved[ch.ID]; ok {
			if ri.localPath != "" {
				icon = ri.localPath
			}
			iconURL = ri.iconURL
		}
		var lcn any
		if ch.LCN != "" {
			if n, err := strconv.Atoi(ch.LCN); err == nil {
				lcn = n
			}
		}
		if _, err := chStmt.Exec(ch.ID, name, icon, i, lcn, iconURL); err != nil {
			return fmt.Errorf("upserting channel %s: %w", ch.ID, err)
		}
	}

	airStmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO airings (
			channel_id, start_time, stop_time,
			title, sub_title, description, categories,
			episode_num, episode_num_display, prog_id,
			star_rating, content_rating, year, icon, country,
			is_repeat, is_premiere
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("preparing airing upsert: %w", err)
	}
	defer airStmt.Close()

	for _, p := range tv.Programmes {
		a := airingFromXMLTV(p)

		cats := []string{}
		if len(a.Categories) > 0 {
			cats = a.Categories
		}
		catsJSON, _ := json.Marshal(cats)

		if _, err := airStmt.Exec(
			a.ChannelID,
			a.Start.UTC().Format(time.RFC3339),
			a.Stop.UTC().Format(time.RFC3339),
			a.Title,
			nullIfEmpty(a.SubTitle),
			nullIfEmpty(a.Description),
			string(catsJSON),
			nullIfEmpty(a.EpisodeNum),
			nullIfEmpty(a.EpisodeNumDisplay),
			nullIfEmpty(a.ProgID),
			nullIfEmpty(a.StarRating),
			nullIfEmpty(a.ContentRating),
			nullIfEmpty(a.Year),
			nullIfEmpty(a.Icon),
			nullIfEmpty(a.Country),
			boolToInt(a.IsRepeat),
			boolToInt(a.IsPremiere),
		); err != nil {
			return fmt.Errorf("upserting airing: %w", err)
		}
	}

	cutoff := time.Now().AddDate(0, 0, -d.retentionDays).UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`DELETE FROM airings WHERE stop_time < ?`, cutoff); err != nil {
		return fmt.Errorf("pruning airings: %w", err)
	}

	// Rebuild FTS index: clear and repopulate from airings table.
	if _, err := tx.Exec(`DELETE FROM airings_fts`); err != nil {
		return fmt.Errorf("clearing FTS index: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO airings_fts (channel_id, start_time, title, sub_title, description)
		SELECT channel_id, start_time, title, COALESCE(sub_title, ''), COALESCE(description, '')
		FROM airings
	`); err != nil {
		return fmt.Errorf("populating FTS index: %w", err)
	}

	// Rebuild categories table from distinct values in airings.categories JSON arrays.
	if _, err := tx.Exec(`DELETE FROM categories`); err != nil {
		return fmt.Errorf("clearing categories: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO categories (name)
		SELECT DISTINCT value FROM airings, json_each(airings.categories)
		WHERE value IS NOT NULL AND value != ''
	`); err != nil {
		return fmt.Errorf("populating categories: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	d.mu.Lock()
	d.lastRefresh = time.Now()
	d.nextRefresh = nextRefresh
	d.mu.Unlock()

	return nil
}

// EnsureChannelIcon ensures the channel's icon is present on disk and returns
// its local file path. Returns ("", nil) when the channel has no icon or does
// not exist. Re-downloads from the stored external URL if the cached file is
// missing.
func (d *DB) EnsureChannelIcon(ctx context.Context, channelID string) (string, error) {
	var localPath, iconURL string
	err := d.db.QueryRowContext(ctx,
		`SELECT COALESCE(icon, ''), COALESCE(icon_url, '') FROM channels WHERE id = ?`,
		channelID,
	).Scan(&localPath, &iconURL)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("querying channel icon: %w", err)
	}
	if iconURL == "" {
		return "", nil
	}

	// Return the cached path if the file still exists.
	if localPath != "" {
		if _, err := os.Stat(localPath); err == nil {
			return localPath, nil
		}
	}

	// File is missing — re-download.
	if d.httpClient == nil || d.imageCacheDir == "" {
		return "", fmt.Errorf("image cache not configured for re-download")
	}
	newPath, err := d.downloadAndSaveIcon(ctx, channelID, iconURL)
	if err != nil {
		return "", fmt.Errorf("re-downloading icon for %s: %w", channelID, err)
	}

	// Update the stored local path.
	if _, err := d.db.ExecContext(ctx,
		`UPDATE channels SET icon = ? WHERE id = ?`, newPath, channelID,
	); err != nil {
		log.Printf("warning: failed to update icon path for channel %s: %v", channelID, err)
	}

	return newPath, nil
}

// downloadAndSaveIcon fetches the image at iconURL, determines its file
// extension from the Content-Type header (falling back to the URL extension or
// ".jpg"), writes it to {imageCacheDir}/channels/{channelID}{ext}, and returns
// the local path.
func (d *DB) downloadAndSaveIcon(ctx context.Context, channelID, iconURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, iconURL, nil)
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Accept", "image/*,*/*")
	req.Header.Set("User-Agent", "xmltvguide/1.0")
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d from %s", resp.StatusCode, iconURL)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading body: %w", err)
	}

	ext := extFromContentType(resp.Header.Get("Content-Type"))
	if ext == "" {
		ext = extFromURL(iconURL)
	}
	if ext == "" {
		ext = ".jpg"
	}

	dir := filepath.Join(d.imageCacheDir, "channels")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating image directory: %w", err)
	}

	localPath := filepath.Join(dir, channelID+ext)
	if err := os.WriteFile(localPath, data, 0644); err != nil {
		return "", fmt.Errorf("writing icon file: %w", err)
	}

	return localPath, nil
}

// extFromContentType maps a MIME type to a file extension.
func extFromContentType(ct string) string {
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	switch ct {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/svg+xml", "image/svg":
		return ".svg"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}

// extFromURL extracts a recognised image file extension from a URL path,
// ignoring query parameters.
func extFromURL(u string) string {
	if i := strings.Index(u, "?"); i >= 0 {
		u = u[:i]
	}
	ext := filepath.Ext(u)
	switch strings.ToLower(ext) {
	case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp":
		return ext
	default:
		return ""
	}
}

// GetChannels returns all channels ordered by their source sort order.
// The Icon field contains the proxy URL (/images/channel/{id}) for channels
// that have an icon; it is empty for channels without one.
func (d *DB) GetChannels() ([]Channel, error) {
	rows, err := d.db.Query(`
		SELECT id, display_name, COALESCE(icon_url, ''), lcn
		FROM channels
		ORDER BY sort_order
	`)
	if err != nil {
		return nil, fmt.Errorf("querying channels: %w", err)
	}
	defer rows.Close()

	channels := []Channel{}
	for rows.Next() {
		var ch Channel
		var lcn sql.NullInt64
		var iconURL string
		if err := rows.Scan(&ch.ID, &ch.DisplayName, &iconURL, &lcn); err != nil {
			return nil, fmt.Errorf("scanning channel: %w", err)
		}
		if iconURL != "" {
			ch.Icon = "/images/channel/" + ch.ID
		}
		if lcn.Valid {
			n := int(lcn.Int64)
			ch.LCN = &n
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

// GetAirings returns all airings that overlap with the given calendar date,
// interpreted in the server's local timezone.
func (d *DB) GetAirings(date time.Time) ([]Airing, error) {
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.Local).UTC()
	dayEnd := dayStart.Add(24 * time.Hour)

	rows, err := d.db.Query(`
		SELECT
			channel_id,
			start_time,
			stop_time,
			title,
			COALESCE(sub_title, ''),
			COALESCE(description, ''),
			COALESCE(categories, '[]'),
			COALESCE(episode_num, ''),
			COALESCE(episode_num_display, ''),
			COALESCE(prog_id, ''),
			COALESCE(star_rating, ''),
			COALESCE(content_rating, ''),
			COALESCE(year, ''),
			COALESCE(icon, ''),
			COALESCE(country, ''),
			is_repeat,
			is_premiere
		FROM airings
		WHERE stop_time > ? AND start_time < ?
		ORDER BY start_time
	`, dayStart.Format(time.RFC3339), dayEnd.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("querying airings: %w", err)
	}
	defer rows.Close()

	var airings []Airing
	for rows.Next() {
		var a Airing
		var startStr, stopStr, catsJSON string
		var isRepeat, isPremiere int

		if err := rows.Scan(
			&a.ChannelID, &startStr, &stopStr,
			&a.Title, &a.SubTitle, &a.Description, &catsJSON,
			&a.EpisodeNum, &a.EpisodeNumDisplay, &a.ProgID,
			&a.StarRating, &a.ContentRating, &a.Year, &a.Icon, &a.Country,
			&isRepeat, &isPremiere,
		); err != nil {
			return nil, fmt.Errorf("scanning airing: %w", err)
		}

		a.Start, _ = time.Parse(time.RFC3339, startStr)
		a.Stop, _ = time.Parse(time.RFC3339, stopStr)
		json.Unmarshal([]byte(catsJSON), &a.Categories) //nolint:errcheck — malformed JSON yields nil slice, which is acceptable
		a.IsRepeat = isRepeat == 1
		a.IsPremiere = isPremiere == 1

		airings = append(airings, a)
	}
	return airings, rows.Err()
}

// GetStatus returns the current refresh metadata.
func (d *DB) GetStatus() Status {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return Status{
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

// airingFromXMLTV maps an xmltv.Programme to an Airing.
func airingFromXMLTV(p xmltv.Programme) Airing {
	a := Airing{
		ChannelID:  p.Channel,
		Start:      p.Start.Time,
		Stop:       p.Stop.Time,
		IsRepeat:   p.PreviouslyShown != nil,
		IsPremiere: p.Premiere != nil || p.New != nil,
	}

	if len(p.Titles) > 0 {
		a.Title = p.Titles[0].Value
	}
	if len(p.SubTitles) > 0 {
		a.SubTitle = p.SubTitles[0].Value
	}
	if len(p.Descs) > 0 {
		a.Description = p.Descs[0].Value
	}
	for _, cat := range p.Categories {
		if cat.Value != "" {
			a.Categories = append(a.Categories, cat.Value)
		}
	}
	for _, en := range p.EpisodeNums {
		switch strings.ToLower(en.System) {
		case "xmltv_ns":
			a.EpisodeNum = strings.TrimSpace(en.Value)
		case "onscreen", "sxxexx":
			a.EpisodeNumDisplay = strings.TrimSpace(en.Value)
		case "dd_progid":
			a.ProgID = strings.TrimSpace(en.Value)
		}
	}
	if len(p.StarRatings) > 0 {
		a.StarRating = p.StarRatings[0].Value
	}
	if len(p.Ratings) > 0 {
		a.ContentRating = p.Ratings[0].Value
	}
	if p.Date != "" {
		// Date may be a full date string — we only want the 4-digit year.
		a.Year = p.Date
		if len(p.Date) > 4 {
			a.Year = p.Date[:4]
		}
	}
	if len(p.Icons) > 0 {
		a.Icon = p.Icons[0].Src
	}
	if len(p.Country) > 0 {
		a.Country = p.Country[0].Value
	}

	return a
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ExecRaw executes a raw SQL statement against the database.
// Exposed for testing (e.g. verifying FTS5 availability).
func (d *DB) ExecRaw(query string) (sql.Result, error) {
	return d.db.Exec(query)
}

// SearchSimple performs an FTS5 search on the title column only.
// Returns future airings ordered by relevance then start time.
// If includeRepeats is false, repeats are excluded.
func (d *DB) SearchSimple(query string, includeRepeats bool) ([]SearchResult, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	q := `
		SELECT
			a.channel_id, a.start_time, a.stop_time,
			a.title, COALESCE(a.sub_title, ''), COALESCE(a.description, ''),
			COALESCE(a.categories, '[]'),
			COALESCE(a.episode_num, ''), COALESCE(a.episode_num_display, ''),
			COALESCE(a.prog_id, ''), COALESCE(a.star_rating, ''),
			COALESCE(a.content_rating, ''), COALESCE(a.year, ''),
			COALESCE(a.icon, ''), COALESCE(a.country, ''),
			a.is_repeat, a.is_premiere,
			c.display_name, f.rank
		FROM airings_fts f
		JOIN airings a ON a.channel_id = f.channel_id AND a.start_time = f.start_time
		JOIN channels c ON c.id = a.channel_id
		WHERE f.title MATCH ?
		  AND a.start_time > ?`
	args := []any{query, now}

	if !includeRepeats {
		q += ` AND a.is_repeat = 0`
	}
	q += ` ORDER BY f.rank, a.start_time`

	return d.scanSearchResults(q, args...)
}

// SearchAdvanced performs an FTS5 search on title, sub_title, and description.
// If categories is non-empty, only airings with at least one matching category are returned.
// If includePast is false, only future airings are returned.
// If includeRepeats is false, repeats are excluded.
func (d *DB) SearchAdvanced(query string, categories []string, includePast bool, includeRepeats bool) ([]SearchResult, error) {
	q := `
		SELECT
			a.channel_id, a.start_time, a.stop_time,
			a.title, COALESCE(a.sub_title, ''), COALESCE(a.description, ''),
			COALESCE(a.categories, '[]'),
			COALESCE(a.episode_num, ''), COALESCE(a.episode_num_display, ''),
			COALESCE(a.prog_id, ''), COALESCE(a.star_rating, ''),
			COALESCE(a.content_rating, ''), COALESCE(a.year, ''),
			COALESCE(a.icon, ''), COALESCE(a.country, ''),
			a.is_repeat, a.is_premiere,
			c.display_name, f.rank
		FROM airings_fts f
		JOIN airings a ON a.channel_id = f.channel_id AND a.start_time = f.start_time
		JOIN channels c ON c.id = a.channel_id
		WHERE airings_fts MATCH ?`
	args := []any{query}

	if !includePast {
		now := time.Now().UTC().Format(time.RFC3339)
		q += ` AND a.start_time > ?`
		args = append(args, now)
	}
	if !includeRepeats {
		q += ` AND a.is_repeat = 0`
	}
	if len(categories) > 0 {
		placeholders := make([]string, len(categories))
		for i, c := range categories {
			placeholders[i] = "?"
			args = append(args, c)
		}
		q += ` AND EXISTS (SELECT 1 FROM json_each(a.categories) WHERE value IN (` + strings.Join(placeholders, ",") + `))`
	}
	q += ` ORDER BY f.rank, a.start_time`

	return d.scanSearchResults(q, args...)
}

// GetCategories returns all distinct categories sorted alphabetically.
func (d *DB) GetCategories() ([]string, error) {
	rows, err := d.db.Query(`SELECT name FROM categories ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("querying categories: %w", err)
	}
	defer rows.Close()

	cats := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scanning category: %w", err)
		}
		cats = append(cats, name)
	}
	return cats, rows.Err()
}

// scanSearchResults executes a query and scans results into SearchResult slices.
// The query must select the standard airing columns plus display_name and rank.
func (d *DB) scanSearchResults(query string, args ...any) ([]SearchResult, error) {
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying search results: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var sr SearchResult
		var startStr, stopStr, catsJSON string
		var isRepeat, isPremiere int

		if err := rows.Scan(
			&sr.ChannelID, &startStr, &stopStr,
			&sr.Title, &sr.SubTitle, &sr.Description, &catsJSON,
			&sr.EpisodeNum, &sr.EpisodeNumDisplay, &sr.ProgID,
			&sr.StarRating, &sr.ContentRating, &sr.Year, &sr.Icon, &sr.Country,
			&isRepeat, &isPremiere,
			&sr.ChannelName, &sr.Rank,
		); err != nil {
			return nil, fmt.Errorf("scanning search result: %w", err)
		}

		sr.Start, _ = time.Parse(time.RFC3339, startStr)
		sr.Stop, _ = time.Parse(time.RFC3339, stopStr)
		json.Unmarshal([]byte(catsJSON), &sr.Categories)
		sr.IsRepeat = isRepeat == 1
		sr.IsPremiere = isPremiere == 1

		results = append(results, sr)
	}
	return results, rows.Err()
}

// scanAirings executes a query and scans results into Airing slices.
func (d *DB) scanAirings(query string, args ...any) ([]Airing, error) {
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying airings: %w", err)
	}
	defer rows.Close()

	var airings []Airing
	for rows.Next() {
		var a Airing
		var startStr, stopStr, catsJSON string
		var isRepeat, isPremiere int

		if err := rows.Scan(
			&a.ChannelID, &startStr, &stopStr,
			&a.Title, &a.SubTitle, &a.Description, &catsJSON,
			&a.EpisodeNum, &a.EpisodeNumDisplay, &a.ProgID,
			&a.StarRating, &a.ContentRating, &a.Year, &a.Icon, &a.Country,
			&isRepeat, &isPremiere,
		); err != nil {
			return nil, fmt.Errorf("scanning airing: %w", err)
		}

		a.Start, _ = time.Parse(time.RFC3339, startStr)
		a.Stop, _ = time.Parse(time.RFC3339, stopStr)
		json.Unmarshal([]byte(catsJSON), &a.Categories)
		a.IsRepeat = isRepeat == 1
		a.IsPremiere = isPremiere == 1

		airings = append(airings, a)
	}
	return airings, rows.Err()
}
