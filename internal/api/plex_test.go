package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/acbgbca/xmltvguide/internal/api"
	"github.com/acbgbca/xmltvguide/internal/database"
	"github.com/acbgbca/xmltvguide/internal/images"
)

func TestPlexStatus_Disabled_ReturnsEnabledFalse(t *testing.T) {
	// No SetPlexStatusFunc registered → endpoint should still exist and
	// return {"enabled": false}.
	srv := newSeededServer(t)

	resp, err := httpGet(t, srv.URL+"/api/plex/status")
	if err != nil {
		t.Fatalf("GET /api/plex/status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if enabled, _ := body["enabled"].(bool); enabled {
		t.Errorf("expected enabled=false, got %v", body)
	}
	// No other fields should be present.
	if _, ok := body["channels"]; ok {
		t.Errorf("expected no channels field when disabled, got %v", body)
	}
	if _, ok := body["lastPoll"]; ok {
		t.Errorf("expected no lastPoll field when disabled, got %v", body)
	}
}

// plexStatusFixture is the canonical snapshot used by the enabled-mode tests
// below — large enough to populate every documented JSON field.
func plexStatusFixture() api.PlexStatusSnapshot {
	return api.PlexStatusSnapshot{
		Enabled:        true,
		LastPoll:       time.Date(2026, 5, 23, 13, 0, 0, 0, time.UTC),
		NextPoll:       time.Date(2026, 5, 24, 1, 0, 0, 0, time.UTC),
		LastDuration:   2300 * time.Millisecond,
		Lineups:        1,
		ChannelMatches: api.PlexMatchStats{Total: 45, Matched: 38, ByID: 32, ByLCN: 4, ByName: 2},
		AiringMatches:  api.PlexMatchStats{Total: 1240, Matched: 1180, ByProgID: 1145, ByStartTime: 35},
		UnmatchedChannels: []api.PlexUnmatchedChannel{
			{PlexID: "plex.foo", DisplayName: "Foo TV", Reason: "no_id_lcn_or_name_match"},
		},
		UnmatchedAirings: []api.PlexUnmatchedAiring{
			{ChannelID: "ch5.au", Title: "Movie B", StartTime: time.Date(2026, 5, 23, 21, 0, 0, 0, time.UTC), Reason: "progid_mismatch"},
		},
		Errors: []string{},
	}
}

// fetchPlexStatusBody starts a real handler stub with the supplied snapshot
// and returns the decoded JSON body.
func fetchPlexStatusBody(t *testing.T, snapshot api.PlexStatusSnapshot) map[string]any {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test", images.NewCache(&http.Client{}, filepath.Join(dir, "images")), nil, nil, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	mux := http.NewServeMux()
	h := api.New(db, 0, nil, api.DeepCheckConfig{})
	h.SetPlexStatusFunc(func() (api.PlexStatusSnapshot, bool) { return snapshot, true })
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := httpGet(t, srv.URL+"/api/plex/status")
	if err != nil {
		t.Fatalf("GET /api/plex/status: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

func TestPlexStatus_Enabled_TopLevelFields(t *testing.T) {
	body := fetchPlexStatusBody(t, plexStatusFixture())
	if enabled, _ := body["enabled"].(bool); !enabled {
		t.Errorf("enabled = %v, want true", body["enabled"])
	}
	if ms, _ := body["lastDurationMs"].(float64); ms != 2300 {
		t.Errorf("lastDurationMs = %v, want 2300", body["lastDurationMs"])
	}
	if got, _ := body["lastPoll"].(string); got != "2026-05-23T13:00:00Z" {
		t.Errorf("lastPoll = %q, want 2026-05-23T13:00:00Z", got)
	}
	if got, _ := body["nextPoll"].(string); got != "2026-05-24T01:00:00Z" {
		t.Errorf("nextPoll = %q, want 2026-05-24T01:00:00Z", got)
	}
}

func TestPlexStatus_Enabled_ChannelsBlock(t *testing.T) {
	body := fetchPlexStatusBody(t, plexStatusFixture())
	channels, ok := body["channels"].(map[string]any)
	if !ok {
		t.Fatalf("channels missing or wrong type: %v", body["channels"])
	}
	expectJSONFloat(t, channels, "total", 45)
	expectJSONFloat(t, channels, "matched", 38)
	expectJSONFloat(t, channels, "byId", 32)
	expectJSONFloat(t, channels, "byLcn", 4)
	expectJSONFloat(t, channels, "byName", 2)
	expectJSONAbsent(t, channels, "byProgId")
	expectJSONAbsent(t, channels, "byStartTime")

	unmatched, ok := channels["unmatched"].([]any)
	if !ok || len(unmatched) != 1 {
		t.Fatalf("channels.unmatched not [1 entry]: %v", channels["unmatched"])
	}
	uc := unmatched[0].(map[string]any)
	if uc["plexId"] != "plex.foo" || uc["displayName"] != "Foo TV" || uc["reason"] != "no_id_lcn_or_name_match" {
		t.Errorf("unmatched channel = %v, missing expected fields", uc)
	}
}

func TestPlexStatus_Enabled_AiringsBlock(t *testing.T) {
	body := fetchPlexStatusBody(t, plexStatusFixture())
	airings, ok := body["airings"].(map[string]any)
	if !ok {
		t.Fatalf("airings missing: %v", body["airings"])
	}
	expectJSONFloat(t, airings, "total", 1240)
	expectJSONFloat(t, airings, "matched", 1180)
	expectJSONFloat(t, airings, "byProgId", 1145)
	expectJSONFloat(t, airings, "byStartTime", 35)
	expectJSONAbsent(t, airings, "byId")
	expectJSONAbsent(t, airings, "byLcn")

	ua, ok := airings["unmatched"].([]any)
	if !ok || len(ua) != 1 {
		t.Fatalf("airings.unmatched not [1 entry]: %v", airings["unmatched"])
	}
	uam := ua[0].(map[string]any)
	if uam["channelId"] != "ch5.au" || uam["title"] != "Movie B" || uam["reason"] != "progid_mismatch" {
		t.Errorf("unmatched airing = %v, missing expected fields", uam)
	}
}

func expectJSONFloat(t *testing.T, m map[string]any, key string, want float64) {
	t.Helper()
	if got, _ := m[key].(float64); got != want {
		t.Errorf("%s = %v, want %v", key, m[key], want)
	}
}

func expectJSONAbsent(t *testing.T, m map[string]any, key string) {
	t.Helper()
	if _, ok := m[key]; ok {
		t.Errorf("expected %s to be absent, got %v", key, m[key])
	}
}
