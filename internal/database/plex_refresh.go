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
	GetLineupChannels(ctx context.Context, epgIdentifier string) ([]plex.LineupChannel, error)
	GetGrid(ctx context.Context, epgIdentifier string, beginsAt, endsAt time.Time) ([]plex.GridEntry, error)
}

// PlexMatchStats summarises how many entities were matched and via which
// source. Per-source counters are populated only for the relevant kind:
// channels populate ByID/ByLCN/ByName; airings populate ByStartTime.
type PlexMatchStats struct {
	Total       int
	Matched     int
	ByID        int
	ByLCN       int
	ByName      int
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

// dvrTask pairs one DVR with its EPG identifier. The identifier is both the
// channels-endpoint path prefix and the grid-endpoint path prefix.
type dvrTask struct {
	epgIdentifier string
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

	if len(dvrs) == 0 {
		logging.Warn("WARN: Plex returned 0 DVRs — Plex Live TV / DVR may not be configured on this server.")
	}
	tasks := buildDVRTasks(dvrs)
	result.Lineups = len(tasks)
	logging.Info(fmt.Sprintf("[plex] poll started (%d lineups discovered)", len(tasks)))

	localChannels, err := d.GetChannels(ctx)
	if err != nil {
		return result, fmt.Errorf("loading channels: %w", err)
	}

	channelMatchMap, channelUpdates := d.matchAllChannels(ctx, client, tasks, localChannels, &result)
	logChannelSummary(result.ChannelMatches, result.UnmatchedChan)

	gridStart := time.Now().UTC()
	gridEnd := gridStart.AddDate(0, 0, d.retentionDays)
	airingUpdates := d.matchAllAirings(ctx, client, tasks, channelMatchMap, gridStart, gridEnd, &result)
	logAiringSummary(result.AiringMatches, result.UnmatchedAirings)

	if err := d.applyPlexUpdates(ctx, channelUpdates, airingUpdates); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("applyPlexUpdates: %v", err))
	}

	logging.Info(fmt.Sprintf("[plex] poll completed in %s", time.Since(result.StartedAt).Round(time.Millisecond)))
	return result, nil
}

// buildDVRTasks emits one task per DVR keyed on its EPG identifier. DVRs
// without an EPG identifier are skipped (they can't be queried).
func buildDVRTasks(dvrs []plex.DVR) []dvrTask {
	tasks := make([]dvrTask, 0, len(dvrs))
	for _, dvr := range dvrs {
		if dvr.EPGIdentifier == "" {
			continue
		}
		tasks = append(tasks, dvrTask{epgIdentifier: dvr.EPGIdentifier})
	}
	return tasks
}

// matchAllChannels walks every DVR's channels endpoint and matches each Plex
// channel against localChannels. Returns the Plex→local ID map and the pending
// channel UPDATEs. Stats and unmatched entries are accumulated on result.
func (d *DB) matchAllChannels(ctx context.Context, client PlexAPI, tasks []dvrTask, localChannels []model.Channel, result *PlexPollResult) (map[string]string, []channelUpdate) {
	channelMatchMap := map[string]string{}
	var updates []channelUpdate
	for _, task := range tasks {
		pcs, err := client.GetLineupChannels(ctx, task.epgIdentifier)
		if err != nil {
			msg := fmt.Sprintf("GetLineupChannels(%s): %v", task.epgIdentifier, err)
			result.Errors = append(result.Errors, msg)
			logPlexFetchError(msg, err)
			continue
		}
		for _, pc := range pcs {
			u, ok := matchSingleChannel(pc, task.epgIdentifier, localChannels, result)
			if ok {
				updates = append(updates, u)
				channelMatchMap[pc.ID] = u.channelID
			}
		}
	}
	if result.ChannelMatches.Total > 0 && result.ChannelMatches.Matched == 0 {
		logging.Warn(fmt.Sprintf("WARN: Plex returned %d channels but none matched TV Guide channels. Check that both sources reference the same lineup (compare channel IDs/LCNs/names).", result.ChannelMatches.Total))
	}
	return channelMatchMap, updates
}

// matchSingleChannel pairs one Plex channel with a local candidate and
// updates result's stats. Returns the pending UPDATE row and ok=true on
// success; ok=false means the channel was logged as unmatched.
func matchSingleChannel(pc plex.LineupChannel, epgIdentifier string, localChannels []model.Channel, result *PlexPollResult) (channelUpdate, bool) {
	result.ChannelMatches.Total++
	matched, src := plex.MatchChannel(pc, localChannels)
	if matched == nil {
		result.UnmatchedChan = append(result.UnmatchedChan, PlexUnmatchedChannel{
			PlexID:      pc.ID,
			DisplayName: pc.DisplayName,
			Reason:      "no_id_lcn_or_name_match",
		})
		return channelUpdate{}, false
	}
	result.ChannelMatches.Matched++
	tickChannelMatchSource(src, &result.ChannelMatches)
	logging.Debug(fmt.Sprintf("channel '%s' matched '%s' via %s", pc.DisplayName, matched.ID, matchSourceName(src)))
	return channelUpdate{
		channelID:     matched.ID,
		plexChannelID: pc.ID,
		plexLineupID:  epgIdentifier,
	}, true
}

// tickChannelMatchSource increments the per-source counter for a channel match.
func tickChannelMatchSource(src plex.MatchSource, stats *PlexMatchStats) {
	switch src {
	case plex.MatchSourceID:
		stats.ByID++
	case plex.MatchSourceLCN:
		stats.ByLCN++
	case plex.MatchSourceName:
		stats.ByName++
	}
}

// matchAllAirings walks unique EPG identifiers, fetches each grid, and matches
// each entry against airings on its already-matched channel.
func (d *DB) matchAllAirings(ctx context.Context, client PlexAPI, tasks []dvrTask, channelMatchMap map[string]string, gridStart, gridEnd time.Time, result *PlexPollResult) []airingUpdate {
	var updates []airingUpdate
	seen := map[string]bool{}
	for _, task := range tasks {
		if task.epgIdentifier == "" || seen[task.epgIdentifier] {
			continue
		}
		seen[task.epgIdentifier] = true
		entries, err := client.GetGrid(ctx, task.epgIdentifier, gridStart, gridEnd)
		if err != nil {
			msg := fmt.Sprintf("GetGrid(%s): %v", task.epgIdentifier, err)
			result.Errors = append(result.Errors, msg)
			logPlexFetchError(msg, err)
			continue
		}
		updates = append(updates, d.matchGridEntries(ctx, entries, channelMatchMap, gridStart, gridEnd, result)...)
	}
	return updates
}

// matchGridEntries groups entries by Media[0].channelIdentifier and pairs each
// group with the airings on the corresponding local channel.
func (d *DB) matchGridEntries(ctx context.Context, entries []plex.GridEntry, channelMatchMap map[string]string, gridStart, gridEnd time.Time, result *PlexPollResult) []airingUpdate {
	byChannel := map[string][]plex.GridEntry{}
	for _, e := range entries {
		media, ok := e.PrimaryMedia()
		if !ok {
			result.AiringMatches.Total++
			result.UnmatchedAirings = append(result.UnmatchedAirings, PlexUnmatchedAiring{
				Title:  e.Title,
				Reason: "no_media",
			})
			continue
		}
		byChannel[media.ChannelIdentifier] = append(byChannel[media.ChannelIdentifier], e)
	}
	var updates []airingUpdate
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
			if u, ok := matchSingleAiring(entry, localChannelID, candidates, result); ok {
				updates = append(updates, u)
			}
		}
	}
	return updates
}

// matchSingleAiring pairs one Plex grid entry with a local airing and updates
// result's stats. Returns ok=false when the entry could not be matched.
func matchSingleAiring(entry plex.GridEntry, localChannelID string, candidates []model.Airing, result *PlexPollResult) (airingUpdate, bool) {
	result.AiringMatches.Total++
	matched, src, reason := plex.MatchAiring(entry, candidates)
	if matched == nil {
		var startTime time.Time
		if media, ok := entry.PrimaryMedia(); ok {
			startTime = time.Unix(media.BeginsAt, 0).UTC()
		}
		result.UnmatchedAirings = append(result.UnmatchedAirings, PlexUnmatchedAiring{
			ChannelID: localChannelID,
			Title:     entry.Title,
			StartTime: startTime,
			Reason:    unmatchedReasonString(reason),
		})
		return airingUpdate{}, false
	}
	result.AiringMatches.Matched++
	tickAiringMatchSource(src, &result.AiringMatches)
	return airingUpdate{
		channelID:     matched.ChannelID,
		startTime:     matched.Start,
		plexRatingKey: entry.RatingKey,
	}, true
}

// tickAiringMatchSource increments the per-source counter for an airing match.
func tickAiringMatchSource(src plex.MatchSource, stats *PlexMatchStats) {
	if src == plex.MatchSourceStartTime {
		stats.ByStartTime++
	}
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

	if err := execChannelUpdates(ctx, tx, channelUpdates); err != nil {
		return err
	}
	if err := execAiringUpdates(ctx, tx, airingUpdates); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit plex tx: %w", err)
	}
	return nil
}

// execChannelUpdates runs all channel UPDATEs inside tx. A no-op when updates is empty.
func execChannelUpdates(ctx context.Context, tx *sql.Tx, updates []channelUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `UPDATE channels SET plex_channel_id = ?, plex_lineup_id = ? WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("prepare channel update: %w", err)
	}
	defer func() {
		if err := stmt.Close(); err != nil {
			log.Printf("closing channel update stmt: %v", err)
		}
	}()
	for _, cu := range updates {
		if _, err := stmt.ExecContext(ctx, cu.plexChannelID, cu.plexLineupID, cu.channelID); err != nil {
			return fmt.Errorf("update channel %s: %w", cu.channelID, err)
		}
	}
	return nil
}

// execAiringUpdates runs all airing UPDATEs inside tx. A no-op when updates is empty.
func execAiringUpdates(ctx context.Context, tx *sql.Tx, updates []airingUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `UPDATE airings SET plex_rating_key = ? WHERE channel_id = ? AND start_time = ?`)
	if err != nil {
		return fmt.Errorf("prepare airing update: %w", err)
	}
	defer func() {
		if err := stmt.Close(); err != nil {
			log.Printf("closing airing update stmt: %v", err)
		}
	}()
	for _, au := range updates {
		startStr := au.startTime.UTC().Format(time.RFC3339)
		if _, err := stmt.ExecContext(ctx, au.plexRatingKey, au.channelID, startStr); err != nil {
			return fmt.Errorf("update airing %s@%s: %w", au.channelID, startStr, err)
		}
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
	logging.Info(fmt.Sprintf("[plex] airings: matched %d/%d (%d by start-time)",
		stats.Matched, stats.Total, stats.ByStartTime))
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
