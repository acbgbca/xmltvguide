package database

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/acbgbca/xmltvguide/internal/xmltv"
)

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

	// Build the set of hidden channel IDs (combining ID-based and LCN-based rules)
	// so airings can be filtered by ID alone without needing per-airing LCN lookups.
	hiddenChannelIDs := map[string]bool{}
	for _, ch := range tv.Channels {
		if d.isChannelHidden(ch.ID, ch.LCN) {
			hiddenChannelIDs[ch.ID] = true
		}
	}

	// Phase 2: Resolve icons — download when URL changed or file is absent.
	// Hidden channels are skipped entirely.
	type resolvedIcon struct{ localPath, iconURL string }
	resolved := map[string]resolvedIcon{}

	for _, ch := range tv.Channels {
		if hiddenChannelIDs[ch.ID] {
			continue
		}
		if len(ch.Icons) == 0 {
			continue
		}
		incomingURL := ch.Icons[0].Src
		existing := currentIcons[ch.ID]

		if d.imageCache == nil {
			// Cache not configured: store the URL for future use, no local file.
			resolved[ch.ID] = resolvedIcon{"", incomingURL}
			continue
		}

		var (
			localPath string
			err       error
		)
		if existing.iconURL == incomingURL {
			// URL unchanged: validate existing file and re-download only if missing.
			localPath, err = d.imageCache.EnsureIcon(ctx, ch.ID, existing.localPath, incomingURL)
		} else {
			// URL changed: always download a fresh copy.
			localPath, err = d.imageCache.Download(ctx, ch.ID, incomingURL)
		}
		if err != nil {
			log.Printf("warning: failed to cache icon for channel %s: %v", ch.ID, err)
			// Store icon_url even if download failed so the handler can
			// re-download on demand.
			resolved[ch.ID] = resolvedIcon{"", incomingURL}
			continue
		}
		resolved[ch.ID] = resolvedIcon{localPath, incomingURL}
	}

	// Phase 3: Write everything to the database in a single transaction.
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete any previously stored data for channels that are now hidden.
	// Airings must be deleted before channels due to the foreign key constraint.
	if len(d.hiddenIDs) > 0 || len(d.hiddenLCNs) > 0 {
		idArgs, lcnArgs, filterSQL := buildHiddenFilter(d.hiddenIDs, d.hiddenLCNs)
		deleteArgs := append(idArgs, lcnArgs...)
		if _, err := tx.Exec(
			`DELETE FROM airings WHERE channel_id IN (SELECT id FROM channels WHERE `+filterSQL+`)`,
			deleteArgs...,
		); err != nil {
			return fmt.Errorf("deleting airings for hidden channels: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM channels WHERE `+filterSQL, deleteArgs...); err != nil {
			return fmt.Errorf("deleting hidden channels: %w", err)
		}
	}

	chStmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO channels (id, display_name, icon, sort_order, lcn, icon_url)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("preparing channel upsert: %w", err)
	}
	defer chStmt.Close()

	for i, ch := range tv.Channels {
		if hiddenChannelIDs[ch.ID] {
			continue
		}
		name := ch.ID
		if len(ch.DisplayNames) > 0 && ch.DisplayNames[0].Value != "" {
			name = ch.DisplayNames[0].Value
		}
		for _, w := range d.stripWords {
			name = stripWordCaseInsensitive(name, w)
		}
		name = strings.TrimSpace(name)
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
		if hiddenChannelIDs[p.Channel] {
			continue
		}
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

	cutoff := d.clock.Now().AddDate(0, 0, -d.retentionDays).UTC().Format(time.RFC3339)
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

// stripWordCaseInsensitive removes all occurrences of word from s, case-insensitively.
// Both s and word are lowercased for comparison; the original casing of non-matched
// characters in s is preserved. The word must be non-empty.
func stripWordCaseInsensitive(s, word string) string {
	if word == "" {
		return s
	}
	sLower := strings.ToLower(s)
	wLower := strings.ToLower(word)
	wLen := len(wLower)
	var result strings.Builder
	for {
		idx := strings.Index(sLower, wLower)
		if idx < 0 {
			result.WriteString(s)
			break
		}
		result.WriteString(s[:idx])
		s = s[idx+wLen:]
		sLower = sLower[idx+wLen:]
	}
	return result.String()
}
