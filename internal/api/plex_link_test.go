package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acbgbca/xmltvguide/internal/api"
	"github.com/acbgbca/xmltvguide/internal/database"
	"github.com/acbgbca/xmltvguide/internal/images"
	"github.com/acbgbca/xmltvguide/internal/plex"
	"github.com/acbgbca/xmltvguide/internal/xmltv"
)

const testPlexEPG = "tv.plex.providers.epg.cloud:4"

// stubPlexAPI implements database.PlexAPI for tests so we can seed
// plex_channel_id / plex_lineup_id without spinning up an HTTP server.
type stubPlexAPI struct {
	dvrs     []plex.DVR
	channels map[string][]plex.LineupChannel
	grid     map[string][]plex.GridEntry
}

func (s *stubPlexAPI) GetDVRs(_ context.Context) ([]plex.DVR, error) {
	return s.dvrs, nil
}
func (s *stubPlexAPI) GetLineupChannels(_ context.Context, epg string) ([]plex.LineupChannel, error) {
	return s.channels[epg], nil
}
func (s *stubPlexAPI) GetGrid(_ context.Context, epg string, _, _ time.Time) ([]plex.GridEntry, error) {
	return s.grid[epg], nil
}

// newPlexLinkServer creates a test API server with two channels: ch1 has Plex
// mapping populated (via RefreshPlex against a stub Plex API); ch2 does not.
// externalURL is wired through SetPlexLinkURL — pass "" to simulate no Plex
// configuration.
func newPlexLinkServer(t *testing.T, externalURL string) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test-source", images.NewCache(&http.Client{}, filepath.Join(dir, "images")), nil, nil, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	tv := &xmltv.TV{
		Channels: []xmltv.Channel{
			{ID: "ch1", DisplayNames: []xmltv.Name{{Value: "ABC"}}, LCN: "2"},
			{ID: "ch2", DisplayNames: []xmltv.Name{{Value: "SBS"}}, LCN: "3"},
		},
	}
	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	stub := &stubPlexAPI{
		dvrs: []plex.DVR{{EPGIdentifier: testPlexEPG}},
		channels: map[string][]plex.LineupChannel{
			testPlexEPG: {{ID: "ch1", VCN: "2", DisplayName: "ABC"}},
		},
	}
	if _, err := db.RefreshPlex(context.Background(), stub); err != nil {
		t.Fatalf("RefreshPlex: %v", err)
	}

	mux := http.NewServeMux()
	h := api.New(db, 0, nil, api.DeepCheckConfig{})
	if externalURL != "" {
		h.SetPlexLinkURL(externalURL)
	}
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})
	return srv
}

func TestPlexLink_MappedChannel_Returns200WithURLs(t *testing.T) {
	srv := newPlexLinkServer(t, "https://plex.example.com")

	resp, err := httpGet(t, srv.URL+"/api/channels/ch1/plex-link")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	web, app := body["webUrl"], body["appUrl"]
	if !strings.HasPrefix(web, "https://plex.example.com/") {
		t.Errorf("webUrl = %q, want prefix https://plex.example.com/", web)
	}
	if !strings.Contains(web, "/web/index.html") {
		t.Errorf("webUrl = %q, want it to contain /web/index.html", web)
	}
	// The lineup id must appear somewhere in the path/fragment — colon is
	// valid in path segments per RFC 3986 so no encoding is required.
	if !strings.Contains(web, "tv.plex.providers.epg.cloud:4") {
		t.Errorf("webUrl = %q, want lineup id present", web)
	}
	// The plex_channel_id is the last path segment.
	if !strings.Contains(web, "ch1") {
		t.Errorf("webUrl = %q, want plex channel id ch1 present", web)
	}
	if !strings.HasPrefix(app, "plex://") {
		t.Errorf("appUrl = %q, want plex:// scheme", app)
	}
	if !strings.Contains(app, "ch1") {
		t.Errorf("appUrl = %q, want plex channel id ch1 present", app)
	}
}

func TestPlexLink_MappedChannel_TrailingSlashOnExternalURL(t *testing.T) {
	srv := newPlexLinkServer(t, "https://plex.example.com/")
	resp, err := httpGet(t, srv.URL+"/api/channels/ch1/plex-link")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if strings.Contains(body["webUrl"], "com//web") {
		t.Errorf("webUrl = %q, trailing slash should be trimmed (no double //)", body["webUrl"])
	}
}

func TestPlexLink_UnmappedChannel_Returns404(t *testing.T) {
	srv := newPlexLinkServer(t, "https://plex.example.com")
	resp, err := httpGet(t, srv.URL+"/api/channels/ch2/plex-link")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for channel without Plex mapping", resp.StatusCode)
	}
}

func TestPlexLink_UnknownChannel_Returns404(t *testing.T) {
	srv := newPlexLinkServer(t, "https://plex.example.com")
	resp, err := httpGet(t, srv.URL+"/api/channels/does-not-exist/plex-link")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for unknown channel", resp.StatusCode)
	}
}

func TestPlexLink_NoExternalURLConfigured_Returns404(t *testing.T) {
	// Plex not configured at all: even a mapped channel should 404 because
	// the server has no base URL to build a link from.
	srv := newPlexLinkServer(t, "")
	resp, err := httpGet(t, srv.URL+"/api/channels/ch1/plex-link")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 when Plex external URL is not configured", resp.StatusCode)
	}
}
