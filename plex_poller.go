package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/acbgbca/xmltvguide/internal/api"
	"github.com/acbgbca/xmltvguide/internal/database"
	"github.com/acbgbca/xmltvguide/internal/logging"
	"github.com/acbgbca/xmltvguide/internal/plex"
)

// plexPollerState holds the latest poll result + ticker schedule so the API
// status endpoint can render it. The struct is concurrency-safe via the mutex.
type plexPollerState struct {
	mu       sync.RWMutex
	last     database.PlexPollResult
	hasRun   bool
	nextPoll time.Time
}

// snapshot returns the current state in the api package's view shape so that
// /api/plex/status can render it directly. The bool reports whether the
// poller is enabled; the caller already knows the cfg.plexURL state but it
// is convenient to encode the answer here.
func (s *plexPollerState) snapshot() (api.PlexStatusSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := api.PlexStatusSnapshot{
		Enabled:  true,
		NextPoll: s.nextPoll,
	}
	if s.hasRun {
		out.LastPoll = s.last.StartedAt
		out.LastDuration = s.last.Duration
		out.Lineups = s.last.Lineups
		out.ChannelMatches = api.PlexMatchStats(s.last.ChannelMatches)
		out.AiringMatches = api.PlexMatchStats(s.last.AiringMatches)
		out.UnmatchedChannels = convertUnmatchedChannels(s.last.UnmatchedChan)
		out.UnmatchedAirings = convertUnmatchedAirings(s.last.UnmatchedAirings)
		out.Errors = append([]string{}, s.last.Errors...)
	}
	return out, true
}

func convertUnmatchedChannels(in []database.PlexUnmatchedChannel) []api.PlexUnmatchedChannel {
	if len(in) == 0 {
		return nil
	}
	out := make([]api.PlexUnmatchedChannel, len(in))
	for i, u := range in {
		out[i] = api.PlexUnmatchedChannel{PlexID: u.PlexID, DisplayName: u.DisplayName, Reason: u.Reason}
	}
	return out
}

func convertUnmatchedAirings(in []database.PlexUnmatchedAiring) []api.PlexUnmatchedAiring {
	if len(in) == 0 {
		return nil
	}
	out := make([]api.PlexUnmatchedAiring, len(in))
	for i, u := range in {
		out[i] = api.PlexUnmatchedAiring{ChannelID: u.ChannelID, Title: u.Title, StartTime: u.StartTime, Reason: u.Reason}
	}
	return out
}

// startPlexPoller probes the Plex server once at startup, then kicks off a
// ticker that calls db.RefreshPlex on every interval. Errors are logged with
// the actionable guidance defined in #283; nothing fatal — the XMLTV path is
// independent.
func startPlexPoller(db *database.DB, client *plex.Client, plexURL string, interval time.Duration, state *plexPollerState) *time.Ticker {
	// Startup probe — non-fatal; ticker will retry on every tick.
	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx); err != nil {
		if errors.Is(err, plex.ErrUnauthorized) {
			logging.Error("ERROR: Plex returned 401 — PLEX_TOKEN appears invalid. Regenerate at https://plex.tv → Account → Authorized Devices.")
		} else {
			logging.Error(fmt.Sprintf("ERROR: Plex unreachable at %s: %v. Plex enrichment disabled until reachable. Verify PLEX_URL and that Plex is running.", plexURL, err))
		}
	}

	state.mu.Lock()
	state.nextPoll = time.Now().Add(interval)
	state.mu.Unlock()

	ticker := time.NewTicker(interval)
	go func() {
		// Note: we do not stop the ticker here — the caller owns it and stops
		// it during shutdown so this goroutine exits cleanly when the range
		// loop ends.
		for range ticker.C {
			runOnePlexPoll(db, client, interval, state)
		}
	}()
	return ticker
}

// runOnePlexPoll executes one poll cycle and stores the result on state.
func runOnePlexPoll(db *database.DB, client *plex.Client, interval time.Duration, state *plexPollerState) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	result, err := db.RefreshPlex(ctx, client)
	if err != nil {
		logging.Error(fmt.Sprintf("ERROR: Plex poll failed: %v", err))
	}
	state.mu.Lock()
	state.last = result
	state.hasRun = true
	state.nextPoll = time.Now().Add(interval)
	state.mu.Unlock()
}

// Compile-time guard: *plex.Client must satisfy the database.PlexAPI interface
// (used by RefreshPlex). If the interface contract drifts this fails fast at
// build time rather than at runtime.
var _ database.PlexAPI = (*plex.Client)(nil)
