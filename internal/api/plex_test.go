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

func TestPlexStatus_Enabled_ReturnsDocumentedShape(t *testing.T) {
	now := time.Date(2026, 5, 23, 13, 0, 0, 0, time.UTC)
	next := time.Date(2026, 5, 24, 1, 0, 0, 0, time.UTC)
	startTime := time.Date(2026, 5, 23, 21, 0, 0, 0, time.UTC)

	snapshot := api.PlexStatusSnapshot{
		Enabled:        true,
		LastPoll:       now,
		NextPoll:       next,
		LastDuration:   2300 * time.Millisecond,
		Lineups:        1,
		ChannelMatches: api.PlexMatchStats{Total: 45, Matched: 38, ByID: 32, ByLCN: 4, ByName: 2},
		AiringMatches:  api.PlexMatchStats{Total: 1240, Matched: 1180, ByProgID: 1145, ByStartTime: 35},
		UnmatchedChannels: []api.PlexUnmatchedChannel{
			{PlexID: "plex.foo", DisplayName: "Foo TV", Reason: "no_id_lcn_or_name_match"},
		},
		UnmatchedAirings: []api.PlexUnmatchedAiring{
			{ChannelID: "ch5.au", Title: "Movie B", StartTime: startTime, Reason: "progid_mismatch"},
		},
		Errors: []string{},
	}

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

	channels, ok := body["channels"].(map[string]any)
	if !ok {
		t.Fatalf("channels missing or wrong type: %v", body["channels"])
	}
	if channels["total"] != float64(45) {
		t.Errorf("channels.total = %v, want 45", channels["total"])
	}
	if channels["matched"] != float64(38) {
		t.Errorf("channels.matched = %v, want 38", channels["matched"])
	}
	if channels["byId"] != float64(32) {
		t.Errorf("channels.byId = %v, want 32", channels["byId"])
	}
	if channels["byLcn"] != float64(4) {
		t.Errorf("channels.byLcn = %v, want 4", channels["byLcn"])
	}
	if channels["byName"] != float64(2) {
		t.Errorf("channels.byName = %v, want 2", channels["byName"])
	}
	// Channel airing-specific fields should NOT appear in the channels block.
	if _, ok := channels["byProgId"]; ok {
		t.Errorf("channels.byProgId should be absent")
	}
	if _, ok := channels["byStartTime"]; ok {
		t.Errorf("channels.byStartTime should be absent")
	}

	unmatched, ok := channels["unmatched"].([]any)
	if !ok || len(unmatched) != 1 {
		t.Fatalf("channels.unmatched not [1 entry]: %v", channels["unmatched"])
	}
	uc := unmatched[0].(map[string]any)
	if uc["plexId"] != "plex.foo" || uc["displayName"] != "Foo TV" || uc["reason"] != "no_id_lcn_or_name_match" {
		t.Errorf("unmatched channel = %v, missing expected fields", uc)
	}

	airings, ok := body["airings"].(map[string]any)
	if !ok {
		t.Fatalf("airings missing: %v", body["airings"])
	}
	if airings["total"] != float64(1240) || airings["matched"] != float64(1180) {
		t.Errorf("airings totals wrong: %v", airings)
	}
	if airings["byProgId"] != float64(1145) {
		t.Errorf("airings.byProgId = %v, want 1145", airings["byProgId"])
	}
	if airings["byStartTime"] != float64(35) {
		t.Errorf("airings.byStartTime = %v, want 35", airings["byStartTime"])
	}
	// Airing channel-specific fields should NOT appear in the airings block.
	if _, ok := airings["byId"]; ok {
		t.Errorf("airings.byId should be absent")
	}
	if _, ok := airings["byLcn"]; ok {
		t.Errorf("airings.byLcn should be absent")
	}

	ua, ok := airings["unmatched"].([]any)
	if !ok || len(ua) != 1 {
		t.Fatalf("airings.unmatched not [1 entry]: %v", airings["unmatched"])
	}
	uam := ua[0].(map[string]any)
	if uam["channelId"] != "ch5.au" || uam["title"] != "Movie B" || uam["reason"] != "progid_mismatch" {
		t.Errorf("unmatched airing = %v, missing expected fields", uam)
	}
}
