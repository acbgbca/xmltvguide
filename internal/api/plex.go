package api

import (
	"net/http"
	"time"
)

// PlexMatchStats summarises Plex match counts for one entity kind. Per-source
// counters are non-zero only for the kind they apply to (channels:
// ByID/ByLCN/ByName; airings: ByProgID/ByStartTime). Zero counters are omitted
// from the JSON response via omitempty.
type PlexMatchStats struct {
	Total       int
	Matched     int
	ByID        int
	ByLCN       int
	ByName      int
	ByProgID    int
	ByStartTime int
}

// PlexUnmatchedChannel is one row of /api/plex/status .channels.unmatched.
type PlexUnmatchedChannel struct {
	PlexID      string
	DisplayName string
	Reason      string
}

// PlexUnmatchedAiring is one row of /api/plex/status .airings.unmatched.
type PlexUnmatchedAiring struct {
	ChannelID string
	Title     string
	StartTime time.Time
	Reason    string
}

// PlexStatusSnapshot is the view of the latest Plex poll cycle that the
// /api/plex/status handler renders. Pass via Handler.SetPlexStatusFunc.
type PlexStatusSnapshot struct {
	Enabled           bool
	LastPoll          time.Time
	NextPoll          time.Time
	LastDuration      time.Duration
	Lineups           int
	ChannelMatches    PlexMatchStats
	AiringMatches     PlexMatchStats
	UnmatchedChannels []PlexUnmatchedChannel
	UnmatchedAirings  []PlexUnmatchedAiring
	Errors            []string
}

// PlexStatusFunc returns the latest Plex poll snapshot. The bool indicates
// whether Plex enrichment is enabled at all. When false the handler returns
// {"enabled": false}.
type PlexStatusFunc func() (PlexStatusSnapshot, bool)

// SetPlexStatusFunc registers the function that supplies the current Plex
// poll snapshot to /api/plex/status. If unset, the endpoint returns
// {"enabled": false} regardless of how the poller is wired.
func (h *Handler) SetPlexStatusFunc(fn PlexStatusFunc) {
	h.plexStatusFn = fn
}

// channelStatsView and airingStatsView mirror PlexMatchStats but with
// kind-specific JSON tags + omitempty so zero counters drop out of the
// response.
type channelStatsView struct {
	Total     int                        `json:"total"`
	Matched   int                        `json:"matched"`
	ByID      int                        `json:"byId,omitempty"`
	ByLCN     int                        `json:"byLcn,omitempty"`
	ByName    int                        `json:"byName,omitempty"`
	Unmatched []plexUnmatchedChannelView `json:"unmatched,omitempty"`
}

type airingStatsView struct {
	Total       int                       `json:"total"`
	Matched     int                       `json:"matched"`
	ByProgID    int                       `json:"byProgId,omitempty"`
	ByStartTime int                       `json:"byStartTime,omitempty"`
	Unmatched   []plexUnmatchedAiringView `json:"unmatched,omitempty"`
}

type plexUnmatchedChannelView struct {
	PlexID      string `json:"plexId"`
	DisplayName string `json:"displayName,omitempty"`
	Reason      string `json:"reason"`
}

type plexUnmatchedAiringView struct {
	ChannelID string    `json:"channelId"`
	Title     string    `json:"title,omitempty"`
	StartTime time.Time `json:"startTime"`
	Reason    string    `json:"reason"`
}

type plexStatusEnabledView struct {
	Enabled        bool             `json:"enabled"`
	LastPoll       time.Time        `json:"lastPoll"`
	NextPoll       time.Time        `json:"nextPoll"`
	LastDurationMs int64            `json:"lastDurationMs"`
	Channels       channelStatsView `json:"channels"`
	Airings        airingStatsView  `json:"airings"`
	Errors         []string         `json:"errors"`
}

type plexStatusDisabledView struct {
	Enabled bool `json:"enabled"`
}

func (h *Handler) getPlexStatus(w http.ResponseWriter, r *http.Request) {
	if h.plexStatusFn == nil {
		writeJSON(w, plexStatusDisabledView{Enabled: false})
		return
	}
	snap, enabled := h.plexStatusFn()
	if !enabled {
		writeJSON(w, plexStatusDisabledView{Enabled: false})
		return
	}

	view := plexStatusEnabledView{
		Enabled:        true,
		LastPoll:       snap.LastPoll,
		NextPoll:       snap.NextPoll,
		LastDurationMs: snap.LastDuration.Milliseconds(),
		Channels: channelStatsView{
			Total:     snap.ChannelMatches.Total,
			Matched:   snap.ChannelMatches.Matched,
			ByID:      snap.ChannelMatches.ByID,
			ByLCN:     snap.ChannelMatches.ByLCN,
			ByName:    snap.ChannelMatches.ByName,
			Unmatched: mapUnmatchedChannels(snap.UnmatchedChannels),
		},
		Airings: airingStatsView{
			Total:       snap.AiringMatches.Total,
			Matched:     snap.AiringMatches.Matched,
			ByProgID:    snap.AiringMatches.ByProgID,
			ByStartTime: snap.AiringMatches.ByStartTime,
			Unmatched:   mapUnmatchedAirings(snap.UnmatchedAirings),
		},
		Errors: snap.Errors,
	}
	if view.Errors == nil {
		view.Errors = []string{}
	}
	writeJSON(w, view)
}

func mapUnmatchedChannels(in []PlexUnmatchedChannel) []plexUnmatchedChannelView {
	if len(in) == 0 {
		return nil
	}
	out := make([]plexUnmatchedChannelView, len(in))
	for i, c := range in {
		out[i] = plexUnmatchedChannelView(c)
	}
	return out
}

func mapUnmatchedAirings(in []PlexUnmatchedAiring) []plexUnmatchedAiringView {
	if len(in) == 0 {
		return nil
	}
	out := make([]plexUnmatchedAiringView, len(in))
	for i, a := range in {
		out[i] = plexUnmatchedAiringView(a)
	}
	return out
}
