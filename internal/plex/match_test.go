package plex_test

import (
	"testing"
	"time"

	"github.com/acbgbca/xmltvguide/internal/model"
	"github.com/acbgbca/xmltvguide/internal/plex"
)

func intPtr(n int) *int { return &n }

func TestMatchChannel(t *testing.T) {
	lcn7 := intPtr(7)
	lcn9 := intPtr(9)

	cases := []struct {
		name       string
		plex       plex.LineupChannel
		candidates []model.Channel
		wantID     string // empty means no match expected
		wantSource plex.MatchSource
	}{
		{
			name: "hit by id",
			plex: plex.LineupChannel{ID: "1", XMLTVID: "abc.xmltv", LCN: "7", DisplayName: "ABC"},
			candidates: []model.Channel{
				{ID: "other.xmltv", DisplayName: "Other", LCN: lcn9},
				{ID: "abc.xmltv", DisplayName: "ABC", LCN: lcn7},
			},
			wantID:     "abc.xmltv",
			wantSource: plex.MatchSourceID,
		},
		{
			name: "hit by lcn",
			plex: plex.LineupChannel{ID: "1", XMLTVID: "no-such-id", LCN: "7", DisplayName: "Totally Different"},
			candidates: []model.Channel{
				{ID: "other.xmltv", DisplayName: "Other", LCN: lcn9},
				{ID: "abc.xmltv", DisplayName: "ABC", LCN: lcn7},
			},
			wantID:     "abc.xmltv",
			wantSource: plex.MatchSourceLCN,
		},
		{
			name: "hit by name case-insensitive",
			plex: plex.LineupChannel{ID: "1", XMLTVID: "no-such-id", LCN: "", DisplayName: "abc"},
			candidates: []model.Channel{
				{ID: "other.xmltv", DisplayName: "Other", LCN: lcn9},
				{ID: "abc.xmltv", DisplayName: "ABC", LCN: lcn7},
			},
			wantID:     "abc.xmltv",
			wantSource: plex.MatchSourceName,
		},
		{
			name:       "miss with no candidates",
			plex:       plex.LineupChannel{ID: "1", XMLTVID: "abc.xmltv", LCN: "7", DisplayName: "ABC"},
			candidates: nil,
			wantID:     "",
			wantSource: plex.MatchSourceNone,
		},
		{
			name: "ambiguous name returns none",
			plex: plex.LineupChannel{ID: "1", XMLTVID: "no-such-id", LCN: "", DisplayName: "ABC"},
			candidates: []model.Channel{
				{ID: "abc1.xmltv", DisplayName: "ABC", LCN: nil},
				{ID: "abc2.xmltv", DisplayName: "abc", LCN: nil},
			},
			wantID:     "",
			wantSource: plex.MatchSourceNone,
		},
		{
			name: "both have lcn but different falls through to name",
			plex: plex.LineupChannel{ID: "1", XMLTVID: "no-such-id", LCN: "7", DisplayName: "ABC"},
			candidates: []model.Channel{
				{ID: "abc.xmltv", DisplayName: "ABC", LCN: lcn9},
			},
			wantID:     "abc.xmltv",
			wantSource: plex.MatchSourceName,
		},
		{
			name: "no match at any tier returns none",
			plex: plex.LineupChannel{ID: "1", XMLTVID: "no-such-id", LCN: "42", DisplayName: "Nothing"},
			candidates: []model.Channel{
				{ID: "abc.xmltv", DisplayName: "ABC", LCN: lcn7},
			},
			wantID:     "",
			wantSource: plex.MatchSourceNone,
		},
		{
			name: "plex has no lcn skips lcn matcher",
			plex: plex.LineupChannel{ID: "1", XMLTVID: "no-such-id", LCN: "", DisplayName: "ABC"},
			candidates: []model.Channel{
				{ID: "abc.xmltv", DisplayName: "ABC", LCN: lcn7},
			},
			wantID:     "abc.xmltv",
			wantSource: plex.MatchSourceName,
		},
		{
			name: "candidate without lcn is skipped by lcn matcher",
			plex: plex.LineupChannel{ID: "1", XMLTVID: "no-such-id", LCN: "7", DisplayName: "ABC"},
			candidates: []model.Channel{
				{ID: "abc.xmltv", DisplayName: "Different", LCN: nil},
			},
			wantID:     "",
			wantSource: plex.MatchSourceNone,
		},
		{
			name: "ambiguous lcn returns none without falling through",
			plex: plex.LineupChannel{ID: "1", XMLTVID: "no-such-id", LCN: "7", DisplayName: "ABC"},
			candidates: []model.Channel{
				{ID: "abc.xmltv", DisplayName: "ABC", LCN: lcn7},
				{ID: "other.xmltv", DisplayName: "Other", LCN: lcn7},
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

func TestMatchAiring(t *testing.T) {
	start := time.Date(2025, 6, 10, 14, 0, 0, 0, time.UTC)
	other := time.Date(2025, 6, 10, 15, 0, 0, 0, time.UTC)

	cases := []struct {
		name       string
		plex       plex.GridEntry
		candidates []model.Airing
		wantID     string // channel_id of the matched airing, empty for no match
		wantStart  time.Time
		wantSource plex.MatchSource
		wantReason plex.UnmatchedReason
	}{
		{
			name: "hit by progid",
			plex: plex.GridEntry{RatingKey: "rk1", BeginsAt: other.Unix(), DdProgID: "EP123"},
			candidates: []model.Airing{
				{ChannelID: "ch1", Start: start, ProgID: "EP999"},
				{ChannelID: "ch1", Start: other, ProgID: "EP123"},
			},
			wantID:     "ch1",
			wantStart:  other,
			wantSource: plex.MatchSourceProgID,
			wantReason: plex.UnmatchedNone,
		},
		{
			name: "hit by start time when no progid",
			plex: plex.GridEntry{RatingKey: "rk1", BeginsAt: start.Unix(), DdProgID: ""},
			candidates: []model.Airing{
				{ChannelID: "ch1", Start: other, ProgID: "EP999"},
				{ChannelID: "ch1", Start: start, ProgID: ""},
			},
			wantID:     "ch1",
			wantStart:  start,
			wantSource: plex.MatchSourceStartTime,
			wantReason: plex.UnmatchedNone,
		},
		{
			name: "progid mismatch does not fall through to start time",
			plex: plex.GridEntry{RatingKey: "rk1", BeginsAt: start.Unix(), DdProgID: "EP123"},
			candidates: []model.Airing{
				{ChannelID: "ch1", Start: start, ProgID: "EP999"},
			},
			wantID:     "",
			wantSource: plex.MatchSourceNone,
			wantReason: plex.UnmatchedProgIDMismatch,
		},
		{
			name:       "no candidates returns unmatched no candidate",
			plex:       plex.GridEntry{RatingKey: "rk1", BeginsAt: start.Unix(), DdProgID: "EP123"},
			candidates: nil,
			wantID:     "",
			wantSource: plex.MatchSourceNone,
			wantReason: plex.UnmatchedNoCandidate,
		},
		{
			name: "start time miss returns unmatched no start time",
			plex: plex.GridEntry{RatingKey: "rk1", BeginsAt: start.Unix(), DdProgID: ""},
			candidates: []model.Airing{
				{ChannelID: "ch1", Start: other, ProgID: ""},
			},
			wantID:     "",
			wantSource: plex.MatchSourceNone,
			wantReason: plex.UnmatchedNoStartTimeMatch,
		},
		{
			name: "candidate progid empty does not match progid path",
			plex: plex.GridEntry{RatingKey: "rk1", BeginsAt: start.Unix(), DdProgID: "EP123"},
			candidates: []model.Airing{
				{ChannelID: "ch1", Start: start, ProgID: ""},
			},
			wantID:     "",
			wantSource: plex.MatchSourceNone,
			wantReason: plex.UnmatchedProgIDMismatch,
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
			gotID, gotStart := "", time.Time{}
			if got != nil {
				gotID = got.ChannelID
				gotStart = got.Start
			}
			if gotID != tc.wantID {
				t.Errorf("matched channel_id: got %q, want %q", gotID, tc.wantID)
			}
			if !gotStart.Equal(tc.wantStart) {
				t.Errorf("matched start: got %v, want %v", gotStart, tc.wantStart)
			}
		})
	}
}
