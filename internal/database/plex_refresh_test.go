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

// newPlexFixtureServer returns an httptest server that serves Plex fixture
// JSON for the four endpoints used by RefreshPlex. lineupBodies is keyed by
// lineup ID; gridBodies is keyed by the full path prefix used by GetGrid
// (e.g. "/grid/L1").
func newPlexFixtureServer(t *testing.T, dvrsBody string, lineupBodies map[string]string, gridBodies map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/identity":
			_, _ = w.Write([]byte(`{"MediaContainer":{}}`))
		case r.URL.Path == "/livetv/dvrs":
			_, _ = w.Write([]byte(dvrsBody))
		case strings.HasPrefix(r.URL.Path, "/epg/lineups/") && strings.HasSuffix(r.URL.Path, "/channels"):
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) < 5 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			body, ok := lineupBodies[parts[3]]
			if !ok {
				body = `{"MediaContainer":{"Channel":[]}}`
			}
			_, _ = w.Write([]byte(body))
		case strings.HasSuffix(r.URL.Path, "/grid"):
			gridKey := strings.TrimSuffix(r.URL.Path, "/grid")
			body, ok := gridBodies[gridKey]
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
//   - abc.com.au   — channel matched by xmltvId; one airing matched by progid,
//     one by start-time; one ProgID column verified intact
//   - seven.com.au — channel matched by lcn; its sole airing remains unmatched
//     because the corresponding grid entry's progid does not match
//   - nine.com.au  — no Plex counterpart, so all three plex_* columns stay NULL
func runEndToEndPoll(t *testing.T) (*database.DB, time.Time, database.PlexPollResult) {
	t.Helper()
	base := time.Now().UTC().Truncate(time.Hour).Add(time.Hour)
	db := seedDBForPlex(t, base)

	dvrs := `{"MediaContainer":{"Dvr":[{"id":1,"lineupIDs":["L1"],"gridKey":"/grid/L1"}]}}`
	lineups := map[string]string{
		"L1": `{"MediaContainer":{"Channel":[
			{"id":"plex.abc","xmltvId":"abc.com.au","lcn":"2","title":"ABC"},
			{"id":"plex.seven","lcn":"7","title":"Seven Network"},
			{"id":"plex.extra","lcn":"42","title":"Extra Channel"}
		]}}`,
	}
	grids := map[string]string{
		"/grid/L1": fmt.Sprintf(`{"MediaContainer":{"Metadata":[
			{"ratingKey":"rk-1","channel":"plex.abc","beginsAt":%d,"endsAt":%d,"title":"News at Noon","ddProgID":"EP000123450001"},
			{"ratingKey":"rk-2","channel":"plex.abc","beginsAt":%d,"endsAt":%d,"title":"Movie A"},
			{"ratingKey":"rk-3","channel":"plex.seven","beginsAt":%d,"endsAt":%d,"title":"Unrelated Show","ddProgID":"EPNOMATCH"}
		]}}`,
			base.Unix(), base.Add(time.Hour).Unix(),
			base.Add(time.Hour).Unix(), base.Add(2*time.Hour).Unix(),
			base.Add(3*time.Hour).Unix(), base.Add(4*time.Hour).Unix(),
		),
	}
	srv := newPlexFixtureServer(t, dvrs, lineups, grids)
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
	if len(result.UnmatchedChan) != 1 || result.UnmatchedChan[0].PlexID != "plex.extra" {
		t.Errorf("UnmatchedChan = %+v, want one entry with PlexID=plex.extra", result.UnmatchedChan)
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
	expectInt(t, "AiringMatches.ByProgID", result.AiringMatches.ByProgID, 1)
	expectInt(t, "AiringMatches.ByStartTime", result.AiringMatches.ByStartTime, 1)
}

// checkChannelRowState validates plex_* columns and unchanged display names
// for every seeded channel.
func checkChannelRowState(t *testing.T, chans []model.Channel) {
	t.Helper()
	for _, c := range chans {
		switch c.ID {
		case "abc.com.au":
			expectPtrEq(t, "abc PlexChannelID", c.PlexChannelID, "plex.abc")
			expectPtrEq(t, "abc PlexLineupID", c.PlexLineupID, "L1")
			if c.DisplayName != "ABC" {
				t.Errorf("abc DisplayName mutated: got %q, want ABC", c.DisplayName)
			}
		case "seven.com.au":
			expectPtrEq(t, "seven PlexChannelID", c.PlexChannelID, "plex.seven")
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

func TestRefreshPlex_PartialFailure_OneLineupErrors(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Hour).Add(time.Hour)
	db := seedDBForPlex(t, base)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/livetv/dvrs":
			_, _ = w.Write([]byte(`{"MediaContainer":{"Dvr":[
				{"id":1,"lineupIDs":["L1","L2"],"gridKey":"/grid/main"}
			]}}`))
		case r.URL.Path == "/epg/lineups/L1/channels":
			w.WriteHeader(http.StatusInternalServerError)
		case r.URL.Path == "/epg/lineups/L2/channels":
			_, _ = w.Write([]byte(`{"MediaContainer":{"Channel":[
				{"id":"plex.abc","xmltvId":"abc.com.au","title":"ABC"}
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
		t.Errorf("ChannelMatches.Matched = %d, want 1 (L2 should still succeed)", result.ChannelMatches.Matched)
	}

	// abc should still be updated despite L1 failing.
	chans, err := db.GetChannels(context.Background())
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}
	updated := false
	for _, ch := range chans {
		if ch.ID == "abc.com.au" && ch.PlexChannelID != nil && *ch.PlexChannelID == "plex.abc" {
			updated = true
		}
	}
	if !updated {
		t.Errorf("abc should have plex_channel_id set despite L1 failure")
	}
}

func TestRefreshPlex_DoesNotMutateXMLTVColumns(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Hour).Add(time.Hour)
	db := seedDBForPlex(t, base)

	// Snapshot pre-refresh state.
	preChans, _ := db.GetChannels(context.Background())
	preAirs, _ := db.GetAirings(context.Background(), base)

	dvrs := `{"MediaContainer":{"Dvr":[{"id":1,"lineupIDs":["L1"],"gridKey":"/grid/L1"}]}}`
	lineups := map[string]string{
		"L1": `{"MediaContainer":{"Channel":[{"id":"plex.abc","xmltvId":"abc.com.au","title":"Different Name"}]}}`,
	}
	grids := map[string]string{
		"/grid/L1": fmt.Sprintf(`{"MediaContainer":{"Metadata":[
			{"ratingKey":"rk-1","channel":"plex.abc","beginsAt":%d,"endsAt":%d,"title":"Plex Says Different","ddProgID":"EP000123450001"}
		]}}`, base.Unix(), base.Add(time.Hour).Unix()),
	}
	srv := newPlexFixtureServer(t, dvrs, lineups, grids)
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
