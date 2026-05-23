package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/acbgbca/xmltvguide/internal/logging"
	"github.com/acbgbca/xmltvguide/internal/model"
	"github.com/acbgbca/xmltvguide/internal/plex"
)

// PlexAPI is the narrow interface RefreshPlex needs from the Plex client.
// *plex.Client satisfies it; tests can stub it.
type PlexAPI interface {
	GetDVRs(ctx context.Context) ([]plex.DVR, error)
	GetLineupChannels(ctx context.Context, lineupID string) ([]plex.LineupChannel, error)
	GetGrid(ctx context.Context, gridKey string, beginsAt, endsAt time.Time) ([]plex.GridEntry, error)
}

// PlexMatchStats summarises how many entities were matched and via which
// source. Per-source counters are populated only for the relevant kind:
// channels populate ByID/ByLCN/ByName; airings populate ByProgID/ByStartTime.
type PlexMatchStats struct {
	Total       int
	Matched     int
	ByID        int
	ByLCN       int
	ByName      int
	ByProgID    int
	ByStartTime int
}

// PlexUnmatchedChannel records a Plex channel that could not be paired with a
// local TV Guide channel. The reason is a short stable token suitable for
// surfacing through the JSON API.
type PlexUnmatchedChannel struct {
	PlexID      string
	DisplayName string
	Reason      string
}

// PlexUnmatchedAiring records a Plex grid entry that could not be paired with
// a local airing on the matched channel.
type PlexUnmatchedAiring struct {
	ChannelID string
	Title     string
	StartTime time.Time
	Reason    string
}

// PlexPollResult is the outcome of one RefreshPlex cycle.
type PlexPollResult struct {
	StartedAt        time.Time
	Duration         time.Duration
	Lineups          int
	ChannelMatches   PlexMatchStats
	AiringMatches    PlexMatchStats
	UnmatchedChan    []PlexUnmatchedChannel
	UnmatchedAirings []PlexUnmatchedAiring
	Errors           []string
}

// channelUpdate is one pending UPDATE row for the channels table.
type channelUpdate struct {
	channelID     string
	plexChannelID string
	plexLineupID  string
}

// airingUpdate is one pending UPDATE row for the airings table.
type airingUpdate struct {
	channelID     string
	startTime     time.Time
	plexRatingKey string
}

// RefreshPlex performs one Plex EPG enrichment cycle: discover DVRs, match
// every Plex channel against the local channels table, then match every Plex
// grid entry against the airings on its already-matched local channel. All
// successful matches are persisted as UPDATEs to plex_channel_id /
// plex_lineup_id / plex_rating_key in a single transaction.
//
// Per-step errors are appended to the returned result.Errors slice and do not
// abort the cycle — partial enrichment is preferred over rolling back
// successful matches. The function returns a non-nil error only when the
// follow-on database transaction itself fails.
func (d *DB) RefreshPlex(ctx context.Context, client PlexAPI) (PlexPollResult, error) {
	result := PlexPollResult{StartedAt: time.Now()}
	defer func() {
		result.Duration = time.Since(result.StartedAt)
	}()

	logging.Info("[plex] poll started")

	dvrs, err := client.GetDVRs(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("GetDVRs: %v", err))
		logPlexFetchError("GetDVRs", err)
		return result, nil
	}

	type dvrTask struct {
		gridKey   string
		lineupIDs []string
	}
	var tasks []dvrTask
	totalLineups := 0
	for _, dvr := range dvrs {
		tasks = append(tasks, dvrTask{gridKey: dvr.GridKey, lineupIDs: dvr.LineupIDs})
		totalLineups += len(dvr.LineupIDs)
	}
	result.Lineups = totalLineups
	if totalLineups == 0 {
		logging.Warn("WARN: Plex returned 0 DVRs — Plex Live TV / DVR may not be configured on this server.")
	}
	logging.Info(fmt.Sprintf("[plex] poll started (%d lineups discovered)", totalLineups))

	// Load local channels once for channel matching.
	localChannels, err := d.GetChannels(ctx)
	if err != nil {
		return result, fmt.Errorf("loading channels: %w", err)
	}

	// channelMatchMap: Plex channel ID → local channel ID. Used to resolve
	// grid entries back to local channels in the airing-match phase.
	channelMatchMap := map[string]string{}
	var channelUpdates []channelUpdate

	for _, task := range tasks {
		for _, lineupID := range task.lineupIDs {
			pcs, err := client.GetLineupChannels(ctx, lineupID)
			if err != nil {
				msg := fmt.Sprintf("GetLineupChannels(%s): %v", lineupID, err)
				result.Errors = append(result.Errors, msg)
				logPlexFetchError(msg, err)
				continue
			}
			for _, pc := range pcs {
				result.ChannelMatches.Total++
				matched, src := plex.MatchChannel(pc, localChannels)
				if matched != nil {
					result.ChannelMatches.Matched++
					switch src {
					case plex.MatchSourceID:
						result.ChannelMatches.ByID++
					case plex.MatchSourceLCN:
						result.ChannelMatches.ByLCN++
					case plex.MatchSourceName:
						result.ChannelMatches.ByName++
					}
					channelUpdates = append(channelUpdates, channelUpdate{
						channelID:     matched.ID,
						plexChannelID: pc.ID,
						plexLineupID:  lineupID,
					})
					channelMatchMap[pc.ID] = matched.ID
					logging.Debug(fmt.Sprintf("channel '%s' matched '%s' via %s", pc.DisplayName, matched.ID, matchSourceName(src)))
				} else {
					result.UnmatchedChan = append(result.UnmatchedChan, PlexUnmatchedChannel{
						PlexID:      pc.ID,
						DisplayName: pc.DisplayName,
						Reason:      "no_id_lcn_or_name_match",
					})
				}
			}
		}
	}
	if result.ChannelMatches.Total > 0 && result.ChannelMatches.Matched == 0 {
		logging.Warn(fmt.Sprintf("WARN: Plex returned %d channels but none matched TV Guide channels. Check that both sources reference the same lineup (compare channel IDs/LCNs/names).", result.ChannelMatches.Total))
	}
	logChannelSummary(result.ChannelMatches, result.UnmatchedChan)

	// Airing phase — per unique grid key.
	gridStart := time.Now().UTC()
	gridEnd := gridStart.AddDate(0, 0, d.retentionDays)
	var airingUpdates []airingUpdate
	gridSeen := map[string]bool{}

	for _, task := range tasks {
		if task.gridKey == "" || gridSeen[task.gridKey] {
			continue
		}
		gridSeen[task.gridKey] = true

		entries, err := client.GetGrid(ctx, task.gridKey, gridStart, gridEnd)
		if err != nil {
			msg := fmt.Sprintf("GetGrid(%s): %v", task.gridKey, err)
			result.Errors = append(result.Errors, msg)
			logPlexFetchError(msg, err)
			continue
		}

		byChannel := map[string][]plex.GridEntry{}
		for _, e := range entries {
			byChannel[e.Channel] = append(byChannel[e.Channel], e)
		}

		for plexChanID, gridEntries := range byChannel {
			localChannelID, ok := channelMatchMap[plexChanID]
			if !ok {
				continue
			}
			candidates, err := d.getAiringsForChannel(ctx, localChannelID, gridStart, gridEnd)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("getAiringsForChannel(%s): %v", localChannelID, err))
				continue
			}
			for _, entry := range gridEntries {
				result.AiringMatches.Total++
				matched, src, reason := plex.MatchAiring(entry, candidates)
				if matched != nil {
					result.AiringMatches.Matched++
					switch src {
					case plex.MatchSourceProgID:
						result.AiringMatches.ByProgID++
					case plex.MatchSourceStartTime:
						result.AiringMatches.ByStartTime++
					}
					airingUpdates = append(airingUpdates, airingUpdate{
						channelID:     matched.ChannelID,
						startTime:     matched.Start,
						plexRatingKey: entry.RatingKey,
					})
				} else {
					result.UnmatchedAirings = append(result.UnmatchedAirings, PlexUnmatchedAiring{
						ChannelID: localChannelID,
						Title:     entry.Title,
						StartTime: time.Unix(entry.BeginsAt, 0).UTC(),
						Reason:    unmatchedReasonString(reason),
					})
				}
			}
		}
	}
	logAiringSummary(result.AiringMatches, result.UnmatchedAirings)

	if err := d.applyPlexUpdates(ctx, channelUpdates, airingUpdates); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("applyPlexUpdates: %v", err))
	}

	logging.Info(fmt.Sprintf("[plex] poll completed in %s", time.Since(result.StartedAt).Round(time.Millisecond)))
	return result, nil
}

// getAiringsForChannel returns the (start, prog_id) pair for every airing on
// channelID whose start_time lies within the [start, end) window. We only
// load the fields MatchAiring needs.
func (d *DB) getAiringsForChannel(ctx context.Context, channelID string, start, end time.Time) ([]model.Airing, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT channel_id, start_time, COALESCE(prog_id, '')
		FROM airings
		WHERE channel_id = ? AND start_time >= ? AND start_time < ?
	`, channelID, start.Format(time.RFC3339), end.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("querying airings for %s: %w", channelID, err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("closing airings-for-channel rows: %v", err)
		}
	}()
	var out []model.Airing
	for rows.Next() {
		var a model.Airing
		var startStr string
		if err := rows.Scan(&a.ChannelID, &startStr, &a.ProgID); err != nil {
			return nil, fmt.Errorf("scanning airing: %w", err)
		}
		a.Start, _ = time.Parse(time.RFC3339, startStr)
		out = append(out, a)
	}
	return out, rows.Err()
}

// applyPlexUpdates writes all collected channel + airing UPDATEs in a single
// transaction so partial failures roll the whole set back.
func (d *DB) applyPlexUpdates(ctx context.Context, channelUpdates []channelUpdate, airingUpdates []airingUpdate) error {
	if len(channelUpdates) == 0 && len(airingUpdates) == 0 {
		return nil
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			log.Printf("plex tx rollback: %v", rbErr)
		}
	}()

	if len(channelUpdates) > 0 {
		stmt, err := tx.PrepareContext(ctx, `UPDATE channels SET plex_channel_id = ?, plex_lineup_id = ? WHERE id = ?`)
		if err != nil {
			return fmt.Errorf("prepare channel update: %w", err)
		}
		for _, cu := range channelUpdates {
			if _, err := stmt.ExecContext(ctx, cu.plexChannelID, cu.plexLineupID, cu.channelID); err != nil {
				_ = stmt.Close()
				return fmt.Errorf("update channel %s: %w", cu.channelID, err)
			}
		}
		if err := stmt.Close(); err != nil {
			log.Printf("closing channel update stmt: %v", err)
		}
	}

	if len(airingUpdates) > 0 {
		stmt, err := tx.PrepareContext(ctx, `UPDATE airings SET plex_rating_key = ? WHERE channel_id = ? AND start_time = ?`)
		if err != nil {
			return fmt.Errorf("prepare airing update: %w", err)
		}
		for _, au := range airingUpdates {
			startStr := au.startTime.UTC().Format(time.RFC3339)
			if _, err := stmt.ExecContext(ctx, au.plexRatingKey, au.channelID, startStr); err != nil {
				_ = stmt.Close()
				return fmt.Errorf("update airing %s@%s: %w", au.channelID, startStr, err)
			}
		}
		if err := stmt.Close(); err != nil {
			log.Printf("closing airing update stmt: %v", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit plex tx: %w", err)
	}
	return nil
}

// matchSourceName returns the short string identifier for a MatchSource,
// used in debug logging.
func matchSourceName(s plex.MatchSource) string {
	switch s {
	case plex.MatchSourceID:
		return "xmltv-id"
	case plex.MatchSourceLCN:
		return "lcn"
	case plex.MatchSourceName:
		return "display-name"
	case plex.MatchSourceProgID:
		return "progid"
	case plex.MatchSourceStartTime:
		return "start-time"
	}
	return "none"
}

// unmatchedReasonString maps an UnmatchedReason to the stable token used in
// API responses and logs.
func unmatchedReasonString(r plex.UnmatchedReason) string {
	switch r {
	case plex.UnmatchedNoCandidate:
		return "no_candidate_airing"
	case plex.UnmatchedNoProgID:
		return "no_progid_in_plex"
	case plex.UnmatchedProgIDMismatch:
		return "progid_mismatch"
	case plex.UnmatchedNoStartTimeMatch:
		return "no_start_time_match"
	}
	return "unknown"
}

// logPlexFetchError emits the actionable log line for an HTTP fetch error.
// Errors that match plex.ErrUnauthorized produce the PLEX_TOKEN guidance.
func logPlexFetchError(context string, err error) {
	if errors.Is(err, plex.ErrUnauthorized) {
		logging.Error("ERROR: Plex returned 401 — PLEX_TOKEN appears invalid. Regenerate at https://plex.tv → Account → Authorized Devices.")
		return
	}
	logging.Warn(fmt.Sprintf("WARN: Plex poll failed (%s): %v.", context, err))
}

// logChannelSummary emits the per-poll INFO summary block for channel matches.
func logChannelSummary(stats PlexMatchStats, unmatched []PlexUnmatchedChannel) {
	logging.Info(fmt.Sprintf("[plex] channels: matched %d/%d (%d by id, %d by lcn, %d by name)",
		stats.Matched, stats.Total, stats.ByID, stats.ByLCN, stats.ByName))
	if len(unmatched) == 0 {
		return
	}
	const maxSample = 5
	sample := make([]string, 0, maxSample)
	for i, u := range unmatched {
		if i >= maxSample {
			break
		}
		sample = append(sample, u.PlexID)
	}
	suffix := ""
	if len(unmatched) > maxSample {
		suffix = fmt.Sprintf(" (and %d more)", len(unmatched)-maxSample)
	}
	logging.Info(fmt.Sprintf("[plex]   unmatched Plex channels: %v%s", sample, suffix))
}

// logAiringSummary emits the per-poll INFO summary block for airing matches.
func logAiringSummary(stats PlexMatchStats, unmatched []PlexUnmatchedAiring) {
	logging.Info(fmt.Sprintf("[plex] airings: matched %d/%d (%d by progid, %d by start-time)",
		stats.Matched, stats.Total, stats.ByProgID, stats.ByStartTime))
	if len(unmatched) == 0 {
		return
	}
	const maxSample = 5
	for i, u := range unmatched {
		if i >= maxSample {
			break
		}
		logging.Info(fmt.Sprintf("[plex]   unmatched sample: %q on %s @ %s (%s)",
			u.Title, u.ChannelID, u.StartTime.Format("15:04"), u.Reason))
	}
	if len(unmatched) > maxSample {
		logging.Info(fmt.Sprintf("[plex]   (and %d more unmatched)", len(unmatched)-maxSample))
	}
}
