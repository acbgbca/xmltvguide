package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/acbgbca/xmltvguide/internal/model"
)

// GetNowNext returns the current and next airing for every channel, ordered
// by the same sort order as GetChannels (sort_order, which respects lcn).
// Either field may be nil if no matching airing exists.
func (d *DB) GetNowNext() ([]model.NowNextEntry, error) {
	now := d.clock.Now().UTC().Format(time.RFC3339)

	// Fetch channels in sort order.
	channels, err := d.GetChannels()
	if err != nil {
		return nil, fmt.Errorf("getting channels: %w", err)
	}

	// Fetch all currently-airing programmes (start_time <= now < stop_time).
	currentRows, err := d.db.Query(`
		SELECT
			channel_id, start_time, stop_time, title,
			COALESCE(sub_title, ''), COALESCE(description, ''),
			COALESCE(categories, '[]'),
			COALESCE(episode_num, ''), COALESCE(episode_num_display, ''),
			COALESCE(prog_id, ''), COALESCE(star_rating, ''),
			COALESCE(content_rating, ''), COALESCE(year, ''),
			COALESCE(icon, ''), COALESCE(country, ''),
			is_repeat, is_premiere
		FROM airings
		WHERE start_time <= ? AND stop_time > ?
	`, now, now)
	if err != nil {
		return nil, fmt.Errorf("querying current airings: %w", err)
	}
	defer currentRows.Close()

	current := map[string]*model.Airing{}
	for currentRows.Next() {
		a, err := scanAiring(currentRows)
		if err != nil {
			return nil, err
		}
		current[a.ChannelID] = a
	}
	if err := currentRows.Err(); err != nil {
		return nil, fmt.Errorf("iterating current airings: %w", err)
	}

	// Fetch the next upcoming airing per channel using a window function.
	nextRows, err := d.db.Query(`
		SELECT
			channel_id, start_time, stop_time, title,
			COALESCE(sub_title, ''), COALESCE(description, ''),
			COALESCE(categories, '[]'),
			COALESCE(episode_num, ''), COALESCE(episode_num_display, ''),
			COALESCE(prog_id, ''), COALESCE(star_rating, ''),
			COALESCE(content_rating, ''), COALESCE(year, ''),
			COALESCE(icon, ''), COALESCE(country, ''),
			is_repeat, is_premiere
		FROM (
			SELECT *,
				ROW_NUMBER() OVER (PARTITION BY channel_id ORDER BY start_time) AS rn
			FROM airings
			WHERE start_time > ?
		)
		WHERE rn = 1
	`, now)
	if err != nil {
		return nil, fmt.Errorf("querying next airings: %w", err)
	}
	defer nextRows.Close()

	next := map[string]*model.Airing{}
	for nextRows.Next() {
		a, err := scanAiring(nextRows)
		if err != nil {
			return nil, err
		}
		next[a.ChannelID] = a
	}
	if err := nextRows.Err(); err != nil {
		return nil, fmt.Errorf("iterating next airings: %w", err)
	}

	entries := make([]model.NowNextEntry, len(channels))
	for i, ch := range channels {
		entries[i] = model.NowNextEntry{
			ChannelID:   ch.ID,
			ChannelName: ch.DisplayName,
			Current:     current[ch.ID],
			Next:        next[ch.ID],
		}
	}
	return entries, nil
}

// scanAiring scans one row (with the standard 17 airing columns) into a model.Airing.
func scanAiring(rows *sql.Rows) (*model.Airing, error) {
	var a model.Airing
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
	json.Unmarshal([]byte(catsJSON), &a.Categories) //nolint:errcheck
	a.IsRepeat = isRepeat == 1
	a.IsPremiere = isPremiere == 1
	return &a, nil
}
