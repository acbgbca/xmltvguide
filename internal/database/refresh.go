package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/acbgbca/xmltvguide/internal/xmltv"
)

// iconState holds icon metadata for a channel — both the stored state queried
// from the database and the resolved state after icon downloading.
type iconState struct{ localPath, iconURL string }

// Refresh atomically loads a parsed XMLTV document into the database,
// then prunes airings older than the configured retention period.
// Existing airings with the same (channel_id, start_time) are replaced
// with fresh data; historical airings not present in the file are left intact.
// Icons are downloaded when the URL has changed or the local file is missing.
func (d *DB) Refresh(ctx context.Context, tv *xmltv.TV, nextRefresh time.Time) error {
	// Phase 1: Query current channel icon state so we can detect URL changes.
	currentIcons := d.queryCurrentIcons(ctx)

	// Build the set of hidden channel IDs (combining ID-based and LCN-based rules)
	// so airings can be filtered by ID alone without needing per-airing LCN lookups.
	hiddenChannelIDs := map[string]bool{}
	for _, ch := range tv.Channels {
		if d.isChannelHidden(ch.ID, ch.LCN) {
			hiddenChannelIDs[ch.ID] = true
		}
	}

	// Phase 2: Write everything to the database in a single transaction.
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			log.Printf("rollback error: %v", rbErr)
		}
	}()

	if err := d.deleteHiddenChannels(ctx, tx); err != nil {
		return err
	}
	if err := d.upsertChannels(ctx, tx, tv.Channels, hiddenChannelIDs, currentIcons); err != nil {
		return err
	}
	if err := d.upsertAirings(ctx, tx, tv.Programmes, hiddenChannelIDs); err != nil {
		return err
	}

	cutoff := d.clock.Now().AddDate(0, 0, -d.retentionDays).UTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, `DELETE FROM airings WHERE stop_time < ?`, cutoff); err != nil {
		return fmt.Errorf("pruning airings: %w", err)
	}

	if err := d.rebuildFTS(ctx, tx); err != nil {
		return err
	}
	if err := d.rebuildCategories(ctx, tx); err != nil {
		return err
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

// queryCurrentIcons returns the stored icon state for all channels.
func (d *DB) queryCurrentIcons(ctx context.Context) map[string]iconState {
	result := map[string]iconState{}
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, COALESCE(icon, ''), COALESCE(icon_url, '') FROM channels`,
	)
	if err != nil {
		return result
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("closing channel rows: %v", err)
		}
	}()
	for rows.Next() {
		var id, localPath, iconURL string
		if rows.Scan(&id, &localPath, &iconURL) == nil {
			result[id] = iconState{localPath, iconURL}
		}
	}
	return result
}

// deleteHiddenChannels removes airings and channel rows for channels that are
// currently configured as hidden. Airings are deleted before channels to satisfy
// the foreign key constraint.
func (d *DB) deleteHiddenChannels(ctx context.Context, tx *sql.Tx) error {
	if len(d.hiddenIDs) == 0 && len(d.hiddenLCNs) == 0 {
		return nil
	}
	idArgs, lcnArgs, filterSQL := buildHiddenFilter(d.hiddenIDs, d.hiddenLCNs)
	deleteArgs := make([]any, 0, len(idArgs)+len(lcnArgs))
	deleteArgs = append(deleteArgs, idArgs...)
	deleteArgs = append(deleteArgs, lcnArgs...)
	if _, err := tx.ExecContext(ctx, //nolint:gosec // filterSQL contains only ? placeholders, no user values
		`DELETE FROM airings WHERE channel_id IN (SELECT id FROM channels WHERE `+filterSQL+`)`,
		deleteArgs...,
	); err != nil {
		return fmt.Errorf("deleting airings for hidden channels: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM channels WHERE `+filterSQL, deleteArgs...); err != nil { //nolint:gosec // filterSQL contains only ? placeholders, no user values
		return fmt.Errorf("deleting hidden channels: %w", err)
	}
	return nil
}

// resolveChannelIcons downloads icons for visible channels, re-downloading only
// when the URL has changed or the local file is missing.
func (d *DB) resolveChannelIcons(ctx context.Context, channels []xmltv.Channel, hiddenIDs map[string]bool, currentIcons map[string]iconState) map[string]iconState {
	resolved := map[string]iconState{}
	for _, ch := range channels {
		if hiddenIDs[ch.ID] || len(ch.Icons) == 0 {
			continue
		}
		incomingURL := ch.Icons[0].Src
		existing := currentIcons[ch.ID]

		if d.imageCache == nil {
			// Cache not configured: store the URL for future use, no local file.
			resolved[ch.ID] = iconState{"", incomingURL}
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
			resolved[ch.ID] = iconState{"", incomingURL}
			continue
		}
		resolved[ch.ID] = iconState{localPath, incomingURL}
	}
	return resolved
}

// channelDisplayName returns the display name for a channel, with configured
// strip words removed and surrounding whitespace trimmed.
func (d *DB) channelDisplayName(ch xmltv.Channel) string {
	name := ch.ID
	if len(ch.DisplayNames) > 0 && ch.DisplayNames[0].Value != "" {
		name = ch.DisplayNames[0].Value
	}
	for _, w := range d.stripWords {
		name = stripWordCaseInsensitive(name, w)
	}
	return strings.TrimSpace(name)
}

// channelLCN parses the raw LCN string and returns it as an integer any value,
// or nil if the string is empty or not a valid integer.
func channelLCN(ch xmltv.Channel) any {
	if ch.LCN != "" {
		if n, err := strconv.Atoi(ch.LCN); err == nil {
			return n
		}
	}
	return nil
}

// iconStateArgs returns the SQL icon and icon_url argument values for a
// channel from the resolved icon map. Both are nil when the channel has no icon.
func iconStateArgs(resolved map[string]iconState, channelID string) (icon, iconURL any) {
	if ri, ok := resolved[channelID]; ok {
		if ri.localPath != "" {
			icon = ri.localPath
		}
		iconURL = ri.iconURL
	}
	return
}

// upsertChannels inserts or replaces channel rows. Icon downloads are handled
// first via resolveChannelIcons. Hidden channels are skipped.
func (d *DB) upsertChannels(ctx context.Context, tx *sql.Tx, channels []xmltv.Channel, hiddenIDs map[string]bool, currentIcons map[string]iconState) error {
	resolved := d.resolveChannelIcons(ctx, channels, hiddenIDs, currentIcons)

	chStmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO channels (id, display_name, icon, sort_order, lcn, icon_url)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("preparing channel upsert: %w", err)
	}
	defer func() {
		if err := chStmt.Close(); err != nil {
			log.Printf("closing channel statement: %v", err)
		}
	}()

	for i, ch := range channels {
		if hiddenIDs[ch.ID] {
			continue
		}
		icon, iconURL := iconStateArgs(resolved, ch.ID)
		if _, err := chStmt.ExecContext(ctx, ch.ID, d.channelDisplayName(ch), icon, i, channelLCN(ch), iconURL); err != nil {
			return fmt.Errorf("upserting channel %s: %w", ch.ID, err)
		}
	}
	return nil
}

// upsertAirings inserts or replaces airing rows, skipping airings for hidden
// channels. Existing airings whose time range overlaps the incoming airing but
// with a different start_time are deleted first to handle schedule shifts.
func (d *DB) upsertAirings(ctx context.Context, tx *sql.Tx, airings []xmltv.Programme, hiddenIDs map[string]bool) error {
	// deleteOverlapStmt removes any existing airing for the same channel whose
	// time range overlaps the incoming airing but has a different start_time.
	// This handles schedule shifts where a show's start time changes slightly —
	// the old record would otherwise remain alongside the new one.
	deleteOverlapStmt, err := tx.PrepareContext(ctx, `
		DELETE FROM airings
		WHERE channel_id = ?
		AND start_time != ?
		AND start_time < ?
		AND stop_time > ?
	`)
	if err != nil {
		return fmt.Errorf("preparing overlap delete: %w", err)
	}
	defer func() {
		if err := deleteOverlapStmt.Close(); err != nil {
			log.Printf("closing overlap delete statement: %v", err)
		}
	}()

	airStmt, err := tx.PrepareContext(ctx, `
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
	defer func() {
		if err := airStmt.Close(); err != nil {
			log.Printf("closing airing statement: %v", err)
		}
	}()

	for _, p := range airings {
		if hiddenIDs[p.Channel] {
			continue
		}
		a := airingFromXMLTV(p)

		startStr := a.Start.UTC().Format(time.RFC3339)
		stopStr := a.Stop.UTC().Format(time.RFC3339)

		if _, err := deleteOverlapStmt.ExecContext(ctx, a.ChannelID, startStr, stopStr, startStr); err != nil {
			return fmt.Errorf("deleting overlapping airings: %w", err)
		}

		cats := []string{}
		if len(a.Categories) > 0 {
			cats = a.Categories
		}
		catsJSON, _ := json.Marshal(cats)

		if _, err := airStmt.ExecContext(ctx,
			a.ChannelID,
			startStr,
			stopStr,
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
	return nil
}

// rebuildFTS clears and repopulates the airings_fts full-text search index
// from the current contents of the airings table.
func (d *DB) rebuildFTS(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM airings_fts`); err != nil {
		return fmt.Errorf("clearing FTS index: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO airings_fts (channel_id, start_time, title, sub_title, description)
		SELECT channel_id, start_time, title, COALESCE(sub_title, ''), COALESCE(description, '')
		FROM airings
	`); err != nil {
		return fmt.Errorf("populating FTS index: %w", err)
	}
	return nil
}

// rebuildCategories clears and repopulates the categories table from distinct
// values in the airings.categories JSON arrays.
func (d *DB) rebuildCategories(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM categories`); err != nil {
		return fmt.Errorf("clearing categories: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO categories (name)
		SELECT DISTINCT value FROM airings, json_each(airings.categories)
		WHERE value IS NOT NULL AND value != ''
	`); err != nil {
		return fmt.Errorf("populating categories: %w", err)
	}
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
