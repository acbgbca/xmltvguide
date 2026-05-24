package plex_test

import (
	"testing"
	"time"

	"github.com/acbgbca/xmltvguide/internal/model"
	"github.com/acbgbca/xmltvguide/internal/plex"
)

func intPtr(n int) *int { return &n }

func TestMatchChannel(t *testing.T) {
	lcn1 := intPtr(1)
	lcn7 := intPtr(7)
	lcn9 := intPtr(9)
	lcn10 := intPtr(10)

	cases := []struct {
		name       string
		plex       plex.LineupChannel
		candidates []model.Channel
		wantID     string // empty means no match expected
		wantSource plex.MatchSource
	}{
		{
			// Plex XMLTV-provider implementations set the channel id to the XMLTV id, so
			// the id tier remains useful for those users; for Plex Cloud EPG the id is a
			// long hex string that won't collide with any local xmltv id and the tier
			// silently no-ops.
			name: "hit by id (xmltv provider style)",
			plex: plex.LineupChannel{ID: "abc.xmltv", VCN: "7", DisplayName: "ABC"},
			candidates: []model.Channel{
				{ID: "other.xmltv", DisplayName: "Other", LCN: lcn9},
				{ID: "abc.xmltv", DisplayName: "ABC", LCN: lcn7},
			},
			wantID:     "abc.xmltv",
			wantSource: plex.MatchSourceID,
		},
		{
			name: "hit by vcn 7 matches lcn 7",
			plex: plex.LineupChannel{ID: "plex-hex-id", VCN: "7", DisplayName: "Totally Different"},
			candidates: []model.Channel{
				{ID: "other.xmltv", DisplayName: "Other", LCN: lcn9},
				{ID: "abc.xmltv", DisplayName: "ABC", LCN: lcn7},
			},
			wantID:     "abc.xmltv",
			wantSource: plex.MatchSourceLCN,
		},
		{
			name: "hit by vcn 001 matches lcn 1 (leading zero strip)",
			plex: plex.LineupChannel{ID: "plex-hex-id", VCN: "001", DisplayName: "Totally Different"},
			candidates: []model.Channel{
				{ID: "other.xmltv", DisplayName: "Other", LCN: lcn9},
				{ID: "ten.xmltv", DisplayName: "Network Ten", LCN: lcn1},
			},
			wantID:     "ten.xmltv",
			wantSource: plex.MatchSourceLCN,
		},
		{
			name: "hit by vcn 010 matches lcn 10",
			plex: plex.LineupChannel{ID: "plex-hex-id", VCN: "010", DisplayName: "Different"},
			candidates: []model.Channel{
				{ID: "ten.xmltv", DisplayName: "Network Ten", LCN: lcn1},
				{ID: "bold.xmltv", DisplayName: "10 Bold", LCN: lcn10},
			},
			wantID:     "bold.xmltv",
			wantSource: plex.MatchSourceLCN,
		},
		{
			name: "non-numeric vcn falls through to name tier",
			plex: plex.LineupChannel{ID: "plex-hex-id", VCN: "abc", DisplayName: "ABC"},
			candidates: []model.Channel{
				{ID: "abc.xmltv", DisplayName: "ABC", LCN: lcn7},
			},
			wantID:     "abc.xmltv",
			wantSource: plex.MatchSourceName,
		},
		{
			name: "empty vcn falls through to name tier",
			plex: plex.LineupChannel{ID: "plex-hex-id", VCN: "", DisplayName: "ABC"},
			candidates: []model.Channel{
				{ID: "abc.xmltv", DisplayName: "ABC", LCN: lcn7},
			},
			wantID:     "abc.xmltv",
			wantSource: plex.MatchSourceName,
		},
		{
			name: "hit by name case-insensitive",
			plex: plex.LineupChannel{ID: "plex-hex-id", VCN: "", DisplayName: "abc"},
			candidates: []model.Channel{
				{ID: "other.xmltv", DisplayName: "Other", LCN: lcn9},
				{ID: "abc.xmltv", DisplayName: "ABC", LCN: lcn7},
			},
			wantID:     "abc.xmltv",
			wantSource: plex.MatchSourceName,
		},
		{
			name:       "miss with no candidates",
			plex:       plex.LineupChannel{ID: "abc.xmltv", VCN: "7", DisplayName: "ABC"},
			candidates: nil,
			wantID:     "",
			wantSource: plex.MatchSourceNone,
		},
		{
			name: "ambiguous name returns none",
			plex: plex.LineupChannel{ID: "plex-hex-id", VCN: "", DisplayName: "ABC"},
			candidates: []model.Channel{
				{ID: "abc1.xmltv", DisplayName: "ABC", LCN: nil},
				{ID: "abc2.xmltv", DisplayName: "abc", LCN: nil},
			},
			wantID:     "",
			wantSource: plex.MatchSourceNone,
		},
		{
			name: "ambiguous lcn returns none without falling through",
			plex: plex.LineupChannel{ID: "plex-hex-id", VCN: "7", DisplayName: "ABC"},
			candidates: []model.Channel{
				{ID: "abc.xmltv", DisplayName: "ABC", LCN: lcn7},
				{ID: "other.xmltv", DisplayName: "Other", LCN: lcn7},
			},
			wantID:     "",
			wantSource: plex.MatchSourceNone,
		},
		{
			name: "no match at any tier returns none",
			plex: plex.LineupChannel{ID: "plex-hex-id", VCN: "42", DisplayName: "Nothing"},
			candidates: []model.Channel{
				{ID: "abc.xmltv", DisplayName: "ABC", LCN: lcn7},
			},
			wantID:     "",
			wantSource: plex.MatchSourceNone,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, src := plex.MatchChannel(tc.plex, tc.candidates)
			if src != tc.wantSource {
				t.Errorf("source: got %v, want %v", src, tc.wantSource)
			}
			gotID := ""
			if got != nil {
				gotID = got.ID
			}
			if gotID != tc.wantID {
				t.Errorf("matched id: got %q, want %q", gotID, tc.wantID)
			}
		})
	}
}

// makeEntry is a helper for MatchAiring tests that wraps a single Media block
// with the supplied beginsAt timestamp around the typical fields.
func makeEntry(ratingKey string, beginsAt int64) plex.GridEntry {
	return plex.GridEntry{
		RatingKey: ratingKey,
		Media: []plex.GridMedia{
			{
				ChannelIdentifier: "plex-channel",
				BeginsAt:          beginsAt,
				EndsAt:            beginsAt + 3600,
			},
		},
	}
}

func TestMatchAiring(t *testing.T) {
	start := time.Date(2025, 6, 10, 14, 0, 0, 0, time.UTC)
	startUnix := start.Unix()

	cases := []struct {
		name       string
		plex       plex.GridEntry
		candidates []model.Airing
		wantStart  time.Time
		wantSource plex.MatchSource
		wantReason plex.UnmatchedReason
	}{
		{
			name: "exact start time match",
			plex: makeEntry("rk1", startUnix),
			candidates: []model.Airing{
				{ChannelID: "ch1", Start: start.Add(2 * time.Hour)},
				{ChannelID: "ch1", Start: start},
			},
			wantStart:  start,
			wantSource: plex.MatchSourceStartTime,
			wantReason: plex.UnmatchedNone,
		},
		{
			name: "+3 minutes is within tolerance",
			plex: makeEntry("rk1", startUnix),
			candidates: []model.Airing{
				{ChannelID: "ch1", Start: start.Add(3 * time.Minute)},
			},
			wantStart:  start.Add(3 * time.Minute),
			wantSource: plex.MatchSourceStartTime,
			wantReason: plex.UnmatchedNone,
		},
		{
			name: "+5 minutes is at boundary and must match",
			plex: makeEntry("rk1", startUnix),
			candidates: []model.Airing{
				{ChannelID: "ch1", Start: start.Add(5 * time.Minute)},
			},
			wantStart:  start.Add(5 * time.Minute),
			wantSource: plex.MatchSourceStartTime,
			wantReason: plex.UnmatchedNone,
		},
		{
			name: "-5 minutes is at boundary and must match",
			plex: makeEntry("rk1", startUnix),
			candidates: []model.Airing{
				{ChannelID: "ch1", Start: start.Add(-5 * time.Minute)},
			},
			wantStart:  start.Add(-5 * time.Minute),
			wantSource: plex.MatchSourceStartTime,
			wantReason: plex.UnmatchedNone,
		},
		{
			name: "+6 minutes is outside tolerance and must miss",
			plex: makeEntry("rk1", startUnix),
			candidates: []model.Airing{
				{ChannelID: "ch1", Start: start.Add(6 * time.Minute)},
			},
			wantSource: plex.MatchSourceNone,
			wantReason: plex.UnmatchedNoStartTimeMatch,
		},
		{
			name: "two candidates within tolerance — closest wins",
			plex: makeEntry("rk1", startUnix),
			candidates: []model.Airing{
				{ChannelID: "ch1", Start: start.Add(4 * time.Minute)},
				{ChannelID: "ch1", Start: start.Add(2 * time.Minute)},
			},
			wantStart:  start.Add(2 * time.Minute),
			wantSource: plex.MatchSourceStartTime,
			wantReason: plex.UnmatchedNone,
		},
		{
			name:       "no candidates returns UnmatchedNoCandidate",
			plex:       makeEntry("rk1", startUnix),
			candidates: nil,
			wantSource: plex.MatchSourceNone,
			wantReason: plex.UnmatchedNoCandidate,
		},
		{
			name: "no media on grid entry returns UnmatchedNoStartTimeMatch",
			plex: plex.GridEntry{RatingKey: "rk1"},
			candidates: []model.Airing{
				{ChannelID: "ch1", Start: start},
			},
			wantSource: plex.MatchSourceNone,
			wantReason: plex.UnmatchedNoStartTimeMatch,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, src, reason := plex.MatchAiring(tc.plex, tc.candidates)
			if src != tc.wantSource {
				t.Errorf("source: got %v, want %v", src, tc.wantSource)
			}
			if reason != tc.wantReason {
				t.Errorf("reason: got %v, want %v", reason, tc.wantReason)
			}
			gotStart := time.Time{}
			if got != nil {
				gotStart = got.Start
			}
			if !gotStart.Equal(tc.wantStart) {
				t.Errorf("matched start: got %v, want %v", gotStart, tc.wantStart)
			}
		})
	}
}
