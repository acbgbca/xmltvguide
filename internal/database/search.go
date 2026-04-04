package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/acbgbca/xmltvguide/internal/model"
)

// ExecRaw executes a raw SQL statement against the database.
// Exposed for testing (e.g. verifying FTS5 availability).
func (d *DB) ExecRaw(query string) (sql.Result, error) {
	return d.db.Exec(query)
}

// SearchSimple performs an FTS5 search on the title column only.
// Returns future airings ordered by relevance then start time.
// If includeRepeats is false, repeats are excluded.
func (d *DB) SearchSimple(query string, includeRepeats bool, today bool) ([]model.SearchResult, error) {
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
		  AND a.stop_time > ?`
	args := []any{query, now}

	if !includeRepeats {
		q += ` AND a.is_repeat = 0`
	}
	if today {
		endOfDay := endOfToday()
		q += ` AND a.start_time < ?`
		args = append(args, endOfDay)
	}
	q += ` ORDER BY f.rank, a.start_time`

	return d.scanSearchResults(q, args...)
}

// SearchAdvanced performs an FTS5 search on title, sub_title, and description.
// If categories is non-empty, only airings with at least one matching category are returned.
// If includePast is false, only future airings are returned.
// If includeRepeats is false, repeats are excluded.
func (d *DB) SearchAdvanced(query string, categories []string, includePast bool, includeRepeats bool, today bool) ([]model.SearchResult, error) {
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
		q += ` AND a.stop_time > ?`
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
	if today {
		endOfDay := endOfToday()
		q += ` AND a.start_time < ?`
		args = append(args, endOfDay)
	}
	q += ` ORDER BY f.rank, a.start_time`

	return d.scanSearchResults(q, args...)
}

// endOfToday returns midnight tonight in the server's local timezone, formatted as RFC3339 in UTC.
func endOfToday() string {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.Local).UTC().Format(time.RFC3339)
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
func (d *DB) scanSearchResults(query string, args ...any) ([]model.SearchResult, error) {
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying search results: %w", err)
	}
	defer rows.Close()

	var results []model.SearchResult
	for rows.Next() {
		var sr model.SearchResult
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
