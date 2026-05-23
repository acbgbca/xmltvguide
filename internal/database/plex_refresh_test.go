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

func TestRefreshPlex_EndToEnd_MatchesChannelsAndAirings(t *testing.T) {
	// Anchor airings just past now so they fall inside the grid window.
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

	// Channel stats.
	if result.ChannelMatches.Total != 3 {
		t.Errorf("ChannelMatches.Total = %d, want 3", result.ChannelMatches.Total)
	}
	if result.ChannelMatches.Matched != 2 {
		t.Errorf("ChannelMatches.Matched = %d, want 2", result.ChannelMatches.Matched)
	}
	if result.ChannelMatches.ByID != 1 {
		t.Errorf("ChannelMatches.ByID = %d, want 1", result.ChannelMatches.ByID)
	}
	if result.ChannelMatches.ByLCN != 1 {
		t.Errorf("ChannelMatches.ByLCN = %d, want 1", result.ChannelMatches.ByLCN)
	}
	if len(result.UnmatchedChan) != 1 || result.UnmatchedChan[0].PlexID != "plex.extra" {
		t.Errorf("UnmatchedChan = %+v, want one entry with PlexID=plex.extra", result.UnmatchedChan)
	}

	// Airing stats.
	if result.AiringMatches.Total != 3 {
		t.Errorf("AiringMatches.Total = %d, want 3", result.AiringMatches.Total)
	}
	if result.AiringMatches.Matched != 2 {
		t.Errorf("AiringMatches.Matched = %d, want 2", result.AiringMatches.Matched)
	}
	if result.AiringMatches.ByProgID != 1 {
		t.Errorf("AiringMatches.ByProgID = %d, want 1", result.AiringMatches.ByProgID)
	}
	if result.AiringMatches.ByStartTime != 1 {
		t.Errorf("AiringMatches.ByStartTime = %d, want 1", result.AiringMatches.ByStartTime)
	}

	// Channels in DB.
	chans, err := db.GetChannels(context.Background())
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}
	mustPlexID := func(id, want string) {
		t.Helper()
		for _, c := range chans {
			if c.ID == id {
				if c.PlexChannelID == nil || *c.PlexChannelID != want {
					t.Errorf("%s PlexChannelID = %v, want %s", id, c.PlexChannelID, want)
				}
				return
			}
		}
		t.Errorf("channel %s not found", id)
	}
	mustPlexID("abc.com.au", "plex.abc")
	mustPlexID("seven.com.au", "plex.seven")

	// Verify nine.com.au stayed NULL.
	for _, c := range chans {
		if c.ID == "nine.com.au" && c.PlexChannelID != nil {
			t.Errorf("nine.com.au PlexChannelID should be nil, got %v", *c.PlexChannelID)
		}
		if c.ID == "abc.com.au" {
			if c.DisplayName != "ABC" {
				t.Errorf("abc DisplayName mutated: got %q, want ABC", c.DisplayName)
			}
			if c.PlexLineupID == nil || *c.PlexLineupID != "L1" {
				t.Errorf("abc PlexLineupID = %v, want L1", c.PlexLineupID)
			}
		}
	}

	// Airings in DB.
	airings, err := db.GetAirings(context.Background(), base)
	if err != nil {
		t.Fatalf("GetAirings: %v", err)
	}
	var foundNews, foundMovie bool
	for _, a := range airings {
		if a.ChannelID == "abc.com.au" && a.Title == "News at Noon" {
			foundNews = true
			if a.PlexRatingKey == nil || *a.PlexRatingKey != "rk-1" {
				t.Errorf("News PlexRatingKey = %v, want rk-1", a.PlexRatingKey)
			}
			if a.ProgID != "EP000123450001" {
				t.Errorf("News ProgID mutated: got %q", a.ProgID)
			}
		}
		if a.ChannelID == "abc.com.au" && a.Title == "Movie A" {
			foundMovie = true
			if a.PlexRatingKey == nil || *a.PlexRatingKey != "rk-2" {
				t.Errorf("Movie PlexRatingKey = %v, want rk-2", a.PlexRatingKey)
			}
		}
		// Morning Show on seven should remain unmatched.
		if a.ChannelID == "seven.com.au" && a.Title == "Morning Show" {
			if a.PlexRatingKey != nil {
				t.Errorf("Morning Show PlexRatingKey should be nil, got %v", *a.PlexRatingKey)
			}
		}
	}
	if !foundNews || !foundMovie {
		t.Fatalf("expected matched airings; News=%v Movie=%v", foundNews, foundMovie)
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
