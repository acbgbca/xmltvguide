package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
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
	sort_order   INTEGER NOT NULL
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

// DB wraps a SQLite database connection.
type DB struct {
	db            *sql.DB
	retentionDays int
	sourceURL     string

	// lastRefresh and nextRefresh are kept in memory — they reflect the current
	// process's poll cycle and reset on restart, which is intentional.
	mu          sync.RWMutex
	lastRefresh time.Time
	nextRefresh time.Time
}

// Open opens (or creates) a SQLite database at path and applies the schema.
func Open(path string, retentionDays int, sourceURL string) (*DB, error) {
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

	return &DB{
		db:            db,
		retentionDays: retentionDays,
		sourceURL:     sourceURL,
	}, nil
}

// Refresh atomically loads a parsed XMLTV document into the database,
// then prunes airings older than the configured retention period.
// Existing airings with the same (channel_id, start_time) are replaced
// with fresh data; historical airings not present in the file are left intact.
func (d *DB) Refresh(tv *xmltv.TV, nextRefresh time.Time) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	chStmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO channels (id, display_name, icon, sort_order)
		VALUES (?, ?, ?, ?)
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
		var icon any
		if len(ch.Icons) > 0 {
			icon = ch.Icons[0].Src
		}
		if _, err := chStmt.Exec(ch.ID, name, icon, i); err != nil {
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

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	d.mu.Lock()
	d.lastRefresh = time.Now()
	d.nextRefresh = nextRefresh
	d.mu.Unlock()

	return nil
}

// GetChannels returns all channels ordered by their source sort order.
func (d *DB) GetChannels() ([]Channel, error) {
	rows, err := d.db.Query(`
		SELECT id, display_name, COALESCE(icon, '')
		FROM channels
		ORDER BY sort_order
	`)
	if err != nil {
		return nil, fmt.Errorf("querying channels: %w", err)
	}
	defer rows.Close()

	var channels []Channel
	for rows.Next() {
		var ch Channel
		if err := rows.Scan(&ch.ID, &ch.DisplayName, &ch.Icon); err != nil {
			return nil, fmt.Errorf("scanning channel: %w", err)
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
		case "onscreen":
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
