package database_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acbgbca/xmltvguide/internal/database"
	"github.com/acbgbca/xmltvguide/internal/images"
	"github.com/acbgbca/xmltvguide/internal/model"
	"github.com/acbgbca/xmltvguide/internal/plex"
	"github.com/acbgbca/xmltvguide/internal/xmltv"
)

const testEPGID = "tv.plex.providers.epg.cloud:4"

// newPlexFixtureServer returns an httptest server that serves Plex fixture
// JSON for the three endpoints used by RefreshPlex. channelsByEPG is keyed by
// epgIdentifier; gridByEPG is keyed by epgIdentifier and returns the body for
// `/{epgIdentifier}/grid`.
func newPlexFixtureServer(t *testing.T, dvrsBody string, channelsByEPG map[string]string, gridByEPG map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/identity":
			_, _ = w.Write([]byte(`{"MediaContainer":{}}`))
		case r.URL.Path == "/livetv/dvrs":
			_, _ = w.Write([]byte(dvrsBody))
		case strings.HasSuffix(r.URL.Path, "/lineups/dvr/channels"):
			epg := strings.TrimPrefix(r.URL.Path, "/")
			epg = strings.TrimSuffix(epg, "/lineups/dvr/channels")
			body, ok := channelsByEPG[epg]
			if !ok {
				body = `{"MediaContainer":{"Channel":[]}}`
			}
			_, _ = w.Write([]byte(body))
		case strings.HasSuffix(r.URL.Path, "/grid"):
			epg := strings.TrimPrefix(r.URL.Path, "/")
			epg = strings.TrimSuffix(epg, "/grid")
			body, ok := gridByEPG[epg]
			if !ok {
				body = `{"MediaContainer":{"Metadata":[]}}`
			}
			_, _ = w.Write([]byte(body))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// seedDBForPlex opens a fresh DB and seeds three channels and three airings
// anchored to base. abc.com.au is matchable by xmltvId; seven.com.au is
// matchable by LCN; nine.com.au has no matching plex entry.
func seedDBForPlex(t *testing.T, base time.Time) *database.DB {
	t.Helper()
	dir := t.TempDir()
	cache := images.NewCache(&http.Client{Transport: &failingTransport{}}, filepath.Join(dir, "images"))
	db, err := database.Open(filepath.Join(dir, "test.db"), 30, "http://test", cache, nil, nil, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	tv := &xmltv.TV{
		Channels: []xmltv.Channel{
			{ID: "abc.com.au", DisplayNames: []xmltv.Name{{Value: "ABC"}}, LCN: "2"},
			{ID: "seven.com.au", DisplayNames: []xmltv.Name{{Value: "Seven"}}, LCN: "7"},
			{ID: "nine.com.au", DisplayNames: []xmltv.Name{{Value: "Nine"}}, LCN: "9"},
		},
		Programmes: []xmltv.Programme{
			{
				Channel: "abc.com.au",
				Start:   xmltv.XmltvTime{Time: base},
				Stop:    xmltv.XmltvTime{Time: base.Add(time.Hour)},
				Titles:  []xmltv.Name{{Value: "News at Noon"}},
				EpisodeNums: []xmltv.EpisodeNum{
					{System: "dd_progid", Value: "EP000123450001"},
				},
			},
			{
				Channel: "abc.com.au",
				Start:   xmltv.XmltvTime{Time: base.Add(time.Hour)},
				Stop:    xmltv.XmltvTime{Time: base.Add(2 * time.Hour)},
				Titles:  []xmltv.Name{{Value: "Movie A"}},
			},
			{
				Channel: "seven.com.au",
				Start:   xmltv.XmltvTime{Time: base},
				Stop:    xmltv.XmltvTime{Time: base.Add(time.Hour)},
				Titles:  []xmltv.Name{{Value: "Morning Show"}},
			},
		},
	}
	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	return db
}

// runEndToEndPoll spins up a DB seeded with three channels + three airings,
// serves the canonical fixture from a stub Plex server, runs one RefreshPlex
// cycle, and returns the poll result alongside the live DB. The fixtures are
// designed to exercise every match outcome in one pass:
//
//   - abc.com.au   — channel matched by id (Plex XMLTV-provider style); two
//     airings matched by start-time (one exact, one +3 min within tolerance)
//   - seven.com.au — channel matched by vcn=7; its sole airing remains
//     unmatched because the Plex grid entry's BeginsAt is +10 min away (out of
//     the ±5 min window)
//   - nine.com.au  — no Plex counterpart, so all three plex_* columns stay NULL
func runEndToEndPoll(t *testing.T) (*database.DB, time.Time, database.PlexPollResult) {
	t.Helper()
	base := time.Now().UTC().Truncate(time.Hour).Add(time.Hour)
	db := seedDBForPlex(t, base)

	dvrs := fmt.Sprintf(`{"MediaContainer":{"Dvr":[{"key":"4","epgIdentifier":%q}]}}`, testEPGID)
	channels := map[string]string{
		testEPGID: `{"MediaContainer":{"Channel":[
			{"id":"abc.com.au","vcn":"002","title":"ABC","callSign":"ABCAUS"},
			{"id":"plex-seven-hex","vcn":"7","title":"Seven Network","callSign":"SEVEN"},
			{"id":"plex-extra-hex","vcn":"42","title":"Extra Channel","callSign":"EXTRA"}
		]}}`,
	}
	grids := map[string]string{
		testEPGID: fmt.Sprintf(`{"MediaContainer":{"Metadata":[
			{"ratingKey":"rk-1","title":"News at Noon","Media":[{"channelIdentifier":"abc.com.au","beginsAt":%d,"endsAt":%d}]},
			{"ratingKey":"rk-2","title":"Movie A","Media":[{"channelIdentifier":"abc.com.au","beginsAt":%d,"endsAt":%d}]},
			{"ratingKey":"rk-3","title":"Unrelated Show","Media":[{"channelIdentifier":"plex-seven-hex","beginsAt":%d,"endsAt":%d}]}
		]}}`,
			base.Unix(), base.Add(time.Hour).Unix(),
			base.Add(time.Hour).Add(3*time.Minute).Unix(), base.Add(2*time.Hour).Unix(),
			base.Add(10*time.Minute).Unix(), base.Add(70*time.Minute).Unix(),
		),
	}
	srv := newPlexFixtureServer(t, dvrs, channels, grids)
	client := plex.NewClient(srv.URL, "tok", &http.Client{Timeout: 2 * time.Second})

	result, err := db.RefreshPlex(context.Background(), client)
	if err != nil {
		t.Fatalf("RefreshPlex: %v", err)
	}
	return db, base, result
}

func TestRefreshPlex_EndToEnd_ChannelStats(t *testing.T) {
	_, _, result := runEndToEndPoll(t)
	checkChannelStats(t, result)
	if len(result.UnmatchedChan) != 1 || result.UnmatchedChan[0].PlexID != "plex-extra-hex" {
		t.Errorf("UnmatchedChan = %+v, want one entry with PlexID=plex-extra-hex", result.UnmatchedChan)
	}
}

func TestRefreshPlex_EndToEnd_AiringStats(t *testing.T) {
	_, _, result := runEndToEndPoll(t)
	checkAiringStats(t, result)
}

func TestRefreshPlex_EndToEnd_ChannelsTablePopulated(t *testing.T) {
	db, _, _ := runEndToEndPoll(t)
	chans, err := db.GetChannels(context.Background())
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}
	checkChannelRowState(t, chans)
}

func TestRefreshPlex_EndToEnd_AiringsTablePopulated(t *testing.T) {
	db, base, _ := runEndToEndPoll(t)
	airings, err := db.GetAirings(context.Background(), base)
	if err != nil {
		t.Fatalf("GetAirings: %v", err)
	}
	checkAiringRowState(t, airings)
}

// checkChannelStats asserts the totals/sources on result.ChannelMatches.
func checkChannelStats(t *testing.T, result database.PlexPollResult) {
	t.Helper()
	expectInt(t, "ChannelMatches.Total", result.ChannelMatches.Total, 3)
	expectInt(t, "ChannelMatches.Matched", result.ChannelMatches.Matched, 2)
	expectInt(t, "ChannelMatches.ByID", result.ChannelMatches.ByID, 1)
	expectInt(t, "ChannelMatches.ByLCN", result.ChannelMatches.ByLCN, 1)
}

// checkAiringStats asserts the totals/sources on result.AiringMatches.
func checkAiringStats(t *testing.T, result database.PlexPollResult) {
	t.Helper()
	expectInt(t, "AiringMatches.Total", result.AiringMatches.Total, 3)
	expectInt(t, "AiringMatches.Matched", result.AiringMatches.Matched, 2)
	expectInt(t, "AiringMatches.ByStartTime", result.AiringMatches.ByStartTime, 2)
}

// checkChannelRowState validates plex_* columns and unchanged display names
// for every seeded channel. plex_lineup_id stores the EPG identifier.
func checkChannelRowState(t *testing.T, chans []model.Channel) {
	t.Helper()
	for _, c := range chans {
		switch c.ID {
		case "abc.com.au":
			expectPtrEq(t, "abc PlexChannelID", c.PlexChannelID, "abc.com.au")
			expectPtrEq(t, "abc PlexLineupID", c.PlexLineupID, testEPGID)
			if c.DisplayName != "ABC" {
				t.Errorf("abc DisplayName mutated: got %q, want ABC", c.DisplayName)
			}
		case "seven.com.au":
			expectPtrEq(t, "seven PlexChannelID", c.PlexChannelID, "plex-seven-hex")
			expectPtrEq(t, "seven PlexLineupID", c.PlexLineupID, testEPGID)
		case "nine.com.au":
			if c.PlexChannelID != nil {
				t.Errorf("nine.com.au PlexChannelID should be nil, got %v", *c.PlexChannelID)
			}
		}
	}
}

// checkAiringRowState validates plex_rating_key on each matched airing and
// confirms the XMLTV ProgID column was not overwritten by Plex.
func checkAiringRowState(t *testing.T, airings []model.Airing) {
	t.Helper()
	var foundNews, foundMovie bool
	for _, a := range airings {
		if a.ChannelID == "abc.com.au" && a.Title == "News at Noon" {
			foundNews = true
			expectPtrEq(t, "News PlexRatingKey", a.PlexRatingKey, "rk-1")
			if a.ProgID != "EP000123450001" {
				t.Errorf("News ProgID mutated: got %q", a.ProgID)
			}
		}
		if a.ChannelID == "abc.com.au" && a.Title == "Movie A" {
			foundMovie = true
			expectPtrEq(t, "Movie PlexRatingKey", a.PlexRatingKey, "rk-2")
		}
		if a.ChannelID == "seven.com.au" && a.Title == "Morning Show" && a.PlexRatingKey != nil {
			t.Errorf("Morning Show PlexRatingKey should be nil, got %v", *a.PlexRatingKey)
		}
	}
	if !foundNews || !foundMovie {
		t.Fatalf("expected matched airings; News=%v Movie=%v", foundNews, foundMovie)
	}
}

func expectInt(t *testing.T, label string, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %d, want %d", label, got, want)
	}
}

func expectPtrEq(t *testing.T, label string, got *string, want string) {
	t.Helper()
	if got == nil || *got != want {
		t.Errorf("%s = %v, want %s", label, got, want)
	}
}

func TestRefreshPlex_PartialFailure_ChannelsEndpointErrors(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Hour).Add(time.Hour)
	db := seedDBForPlex(t, base)

	const failingEPG = "tv.plex.providers.epg.cloud:1"
	const okEPG = "tv.plex.providers.epg.cloud:2"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/livetv/dvrs":
			_, _ = fmt.Fprintf(w, `{"MediaContainer":{"Dvr":[
				{"key":"1","epgIdentifier":%q},
				{"key":"2","epgIdentifier":%q}
			]}}`, failingEPG, okEPG)
		case r.URL.Path == "/"+failingEPG+"/lineups/dvr/channels":
			w.WriteHeader(http.StatusInternalServerError)
		case r.URL.Path == "/"+okEPG+"/lineups/dvr/channels":
			_, _ = w.Write([]byte(`{"MediaContainer":{"Channel":[
				{"id":"abc.com.au","title":"ABC","vcn":"2"}
			]}}`))
		case strings.HasSuffix(r.URL.Path, "/grid"):
			_, _ = w.Write([]byte(`{"MediaContainer":{"Metadata":[]}}`))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"MediaContainer":{}}`))
		}
	}))
	t.Cleanup(srv.Close)
	client := plex.NewClient(srv.URL, "tok", &http.Client{Timeout: 2 * time.Second})

	result, err := db.RefreshPlex(context.Background(), client)
	if err != nil {
		t.Fatalf("RefreshPlex returned fatal error %v; per-step errors should not abort", err)
	}
	if len(result.Errors) == 0 {
		t.Errorf("expected at least one per-step error in result, got %+v", result.Errors)
	}
	if result.ChannelMatches.Matched != 1 {
		t.Errorf("ChannelMatches.Matched = %d, want 1 (second EPG should still succeed)", result.ChannelMatches.Matched)
	}

	// abc should still be updated despite the first EPG failing.
	chans, err := db.GetChannels(context.Background())
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}
	updated := false
	for _, ch := range chans {
		if ch.ID == "abc.com.au" && ch.PlexChannelID != nil && *ch.PlexChannelID == "abc.com.au" {
			updated = true
		}
	}
	if !updated {
		t.Errorf("abc should have plex_channel_id set despite first EPG failure")
	}
}

func TestRefreshPlex_DoesNotMutateXMLTVColumns(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Hour).Add(time.Hour)
	db := seedDBForPlex(t, base)

	// Snapshot pre-refresh state.
	preChans, _ := db.GetChannels(context.Background())
	preAirs, _ := db.GetAirings(context.Background(), base)

	dvrs := fmt.Sprintf(`{"MediaContainer":{"Dvr":[{"key":"4","epgIdentifier":%q}]}}`, testEPGID)
	channels := map[string]string{
		testEPGID: `{"MediaContainer":{"Channel":[{"id":"abc.com.au","title":"Different Name","vcn":"2"}]}}`,
	}
	grids := map[string]string{
		testEPGID: fmt.Sprintf(`{"MediaContainer":{"Metadata":[
			{"ratingKey":"rk-1","title":"Plex Says Different","Media":[{"channelIdentifier":"abc.com.au","beginsAt":%d,"endsAt":%d}]}
		]}}`, base.Unix(), base.Add(time.Hour).Unix()),
	}
	srv := newPlexFixtureServer(t, dvrs, channels, grids)
	client := plex.NewClient(srv.URL, "tok", &http.Client{Timeout: 2 * time.Second})

	if _, err := db.RefreshPlex(context.Background(), client); err != nil {
		t.Fatalf("RefreshPlex: %v", err)
	}

	postChans, _ := db.GetChannels(context.Background())
	postAirs, _ := db.GetAirings(context.Background(), base)

	// Channel XMLTV columns unchanged.
	if len(preChans) != len(postChans) {
		t.Fatalf("channel count changed: %d → %d", len(preChans), len(postChans))
	}
	for i := range preChans {
		if preChans[i].DisplayName != postChans[i].DisplayName {
			t.Errorf("channel[%d].DisplayName changed: %q → %q", i, preChans[i].DisplayName, postChans[i].DisplayName)
		}
	}
	// Airing XMLTV columns unchanged.
	preB, _ := json.Marshal(stripPlexFields(preAirs))
	postB, _ := json.Marshal(stripPlexFields(postAirs))
	if string(preB) != string(postB) {
		t.Errorf("airing XMLTV columns changed:\npre = %s\npost = %s", preB, postB)
	}
}

// stripPlexFields returns a copy of airings with PlexRatingKey cleared so we
// can compare the XMLTV-derived columns alone.
func stripPlexFields(airings []model.Airing) []model.Airing {
	out := make([]model.Airing, len(airings))
	for i, a := range airings {
		a.PlexRatingKey = nil
		out[i] = a
	}
	return out
}

func TestRefreshPlex_NoOpOnZeroDVRs(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Hour).Add(time.Hour)
	db := seedDBForPlex(t, base)

	srv := newPlexFixtureServer(t,
		`{"MediaContainer":{"Dvr":[]}}`,
		nil, nil,
	)
	client := plex.NewClient(srv.URL, "tok", &http.Client{Timeout: 2 * time.Second})

	result, err := db.RefreshPlex(context.Background(), client)
	if err != nil {
		t.Fatalf("RefreshPlex: %v", err)
	}
	if result.ChannelMatches.Total != 0 || result.ChannelMatches.Matched != 0 {
		t.Errorf("expected zero channel matches, got %+v", result.ChannelMatches)
	}
	if result.AiringMatches.Total != 0 {
		t.Errorf("expected zero airing matches, got %+v", result.AiringMatches)
	}

	// Nothing should be persisted on either channels or airings.
	chans, _ := db.GetChannels(context.Background())
	for _, ch := range chans {
		if ch.PlexChannelID != nil {
			t.Errorf("channel %s should not have PlexChannelID set", ch.ID)
		}
	}
}
