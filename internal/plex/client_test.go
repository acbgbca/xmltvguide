package plex_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acbgbca/xmltvguide/internal/plex"
)

// startServer wraps httptest.NewServer with cleanup.
func startServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func newClient(t *testing.T, baseURL, token string) *plex.Client {
	t.Helper()
	return plex.NewClient(baseURL, token, &http.Client{Timeout: 2 * time.Second})
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	// #nosec G304 -- test-only fixture loader; name is always a hardcoded constant from this test file.
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return string(b)
}

// --- Ping ---

func TestPing_Success(t *testing.T) {
	srv := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/identity" {
			t.Errorf("expected /identity, got %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Plex-Token"); got != "tok" {
			t.Errorf("missing/wrong X-Plex-Token: %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("expected Accept application/json, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"MediaContainer":{"machineIdentifier":"abc"}}`))
	}))

	c := newClient(t, srv.URL, "tok")
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: unexpected error: %v", err)
	}
}

func TestPing_Unauthorized(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			srv := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			}))
			c := newClient(t, srv.URL, "tok")
			err := c.Ping(context.Background())
			if !errors.Is(err, plex.ErrUnauthorized) {
				t.Errorf("expected ErrUnauthorized for %d, got %v", code, err)
			}
		})
	}
}

func TestPing_ServerErrorWrapsURL(t *testing.T) {
	srv := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	c := newClient(t, srv.URL, "tok")
	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error for 500")
	}
	if !strings.Contains(err.Error(), "/identity") {
		t.Errorf("error should mention URL path; got: %v", err)
	}
}

// --- GetDVRs ---

func TestGetDVRs_HappyPath(t *testing.T) {
	body := readFixture(t, "dvrs.json")
	srv := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/livetv/dvrs" {
			t.Errorf("expected /livetv/dvrs, got %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Plex-Token"); got != "tok" {
			t.Errorf("missing X-Plex-Token: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))

	c := newClient(t, srv.URL, "tok")
	dvrs, err := c.GetDVRs(context.Background())
	if err != nil {
		t.Fatalf("GetDVRs: %v", err)
	}
	if len(dvrs) != 1 {
		t.Fatalf("expected 1 DVR, got %d", len(dvrs))
	}
	if dvrs[0].Key != "4" {
		t.Errorf("dvrs[0].Key = %q, want %q", dvrs[0].Key, "4")
	}
	if dvrs[0].EPGIdentifier != "tv.plex.providers.epg.cloud:4" {
		t.Errorf("dvrs[0].EPGIdentifier = %q", dvrs[0].EPGIdentifier)
	}
}

func TestGetDVRs_Unauthorized(t *testing.T) {
	srv := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	c := newClient(t, srv.URL, "tok")
	_, err := c.GetDVRs(context.Background())
	if !errors.Is(err, plex.ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}

func TestGetDVRs_5xxWrapsURL(t *testing.T) {
	srv := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	c := newClient(t, srv.URL, "tok")
	_, err := c.GetDVRs(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "/livetv/dvrs") {
		t.Errorf("error should include request URL; got: %v", err)
	}
}

func TestGetDVRs_MalformedJSON(t *testing.T) {
	srv := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json{`))
	}))
	c := newClient(t, srv.URL, "tok")
	_, err := c.GetDVRs(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "json") {
		t.Errorf("error should mention json decode; got: %v", err)
	}
}

// --- GetLineupChannels ---

func TestGetLineupChannels_HappyPath(t *testing.T) {
	body := readFixture(t, "channels.json")
	const epgID = "tv.plex.providers.epg.cloud:4"
	wantPath := "/" + epgID + "/lineups/dvr/channels"

	srv := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))

	c := newClient(t, srv.URL, "tok")
	chans, err := c.GetLineupChannels(context.Background(), epgID)
	if err != nil {
		t.Fatalf("GetLineupChannels: %v", err)
	}
	if len(chans) != 3 {
		t.Fatalf("expected 3 channels, got %d", len(chans))
	}
	first := chans[0]
	if first.ID != "5fc76c607dc1e8002d48a65d-5fc705f4dd53a6002d8f9173" {
		t.Errorf("chans[0].ID = %q", first.ID)
	}
	if first.VCN != "001" {
		t.Errorf("chans[0].VCN = %q, want %q", first.VCN, "001")
	}
	if first.DisplayName != "Network Ten (Australia)" {
		t.Errorf("chans[0].DisplayName = %q", first.DisplayName)
	}
	if first.CallSign != "THMEANZ" {
		t.Errorf("chans[0].CallSign = %q", first.CallSign)
	}
}

func TestGetLineupChannels_Unauthorized(t *testing.T) {
	srv := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	c := newClient(t, srv.URL, "tok")
	_, err := c.GetLineupChannels(context.Background(), "tv.plex.providers.epg.cloud:4")
	if !errors.Is(err, plex.ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}

// --- GetGrid ---

func TestGetGrid_HappyPath(t *testing.T) {
	body := readFixture(t, "grid.json")
	const epgID = "tv.plex.providers.epg.cloud:4"
	wantPathPrefix := "/" + epgID + "/grid"
	begins := time.Date(2025, 6, 10, 14, 0, 0, 0, time.UTC) // 1749564000
	ends := begins.Add(24 * time.Hour)                      // 1749650400

	srv := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wantPathPrefix {
			t.Errorf("unexpected path: %s, want %s", r.URL.Path, wantPathPrefix)
		}
		raw := r.URL.RawQuery
		if !strings.Contains(raw, "type=4") {
			t.Errorf("raw query missing type=4: %q", raw)
		}
		// The Plex grid query requires comparison operators on beginsAt/endsAt
		// (without them Plex silently returns size:0). The operator characters
		// `>` and `<` must be percent-encoded as %3E and %3C in the raw query.
		if !strings.Contains(raw, "beginsAt%3E=") {
			t.Errorf("raw query should encode beginsAt with > operator; got %q", raw)
		}
		if !strings.Contains(raw, "endsAt%3C=") {
			t.Errorf("raw query should encode endsAt with < operator; got %q", raw)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))

	c := newClient(t, srv.URL, "tok")
	entries, err := c.GetGrid(context.Background(), epgID, begins, ends)
	if err != nil {
		t.Fatalf("GetGrid: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	if entries[0].RatingKey != "plex%3A%2F%2Fepisode%2F6a01ce4627f52460ad3410f4" {
		t.Errorf("entries[0].RatingKey = %q", entries[0].RatingKey)
	}
	if entries[0].Title != "MasterChef Australia" {
		t.Errorf("entries[0].Title = %q", entries[0].Title)
	}
	media, ok := entries[0].PrimaryMedia()
	if !ok {
		t.Fatalf("entries[0].PrimaryMedia() returned ok=false")
	}
	if media.ChannelIdentifier != "5fc76c607dc1e8002d48a65d-5fc705f4dd53a6002d8f9173" {
		t.Errorf("media.ChannelIdentifier = %q", media.ChannelIdentifier)
	}
	if media.BeginsAt != 1749564000 {
		t.Errorf("media.BeginsAt = %d, want 1749564000", media.BeginsAt)
	}
	if media.EndsAt != 1749567600 {
		t.Errorf("media.EndsAt = %d, want 1749567600", media.EndsAt)
	}
}

func TestGetGrid_PrimaryMediaEmpty(t *testing.T) {
	entry := plex.GridEntry{RatingKey: "rk", Title: "no media"}
	if _, ok := entry.PrimaryMedia(); ok {
		t.Errorf("expected ok=false for entry with no Media")
	}
}

func TestGetGrid_PassesUnixTimes(t *testing.T) {
	begins := time.Date(2025, 6, 10, 0, 0, 0, 0, time.UTC)
	ends := time.Date(2025, 6, 11, 0, 0, 0, 0, time.UTC)

	var captured string
	srv := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"MediaContainer":{"Metadata":[]}}`))
	}))

	c := newClient(t, srv.URL, "tok")
	if _, err := c.GetGrid(context.Background(), "tv.plex.providers.epg.cloud:4", begins, ends); err != nil {
		t.Fatalf("GetGrid: %v", err)
	}

	// 1749513600 = 2025-06-10 00:00:00 UTC; 1749600000 = 2025-06-11 00:00:00 UTC
	if !strings.Contains(captured, "beginsAt%3E=1749513600") {
		t.Errorf("raw query missing beginsAt%%3E=1749513600: %q", captured)
	}
	if !strings.Contains(captured, "endsAt%3C=1749600000") {
		t.Errorf("raw query missing endsAt%%3C=1749600000: %q", captured)
	}
}

func TestGetGrid_NetworkTimeoutWraps(t *testing.T) {
	// Server delays beyond the client timeout.
	srv := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	c := plex.NewClient(srv.URL, "tok", &http.Client{Timeout: 50 * time.Millisecond})

	_, err := c.GetGrid(context.Background(), "tv.plex.providers.epg.cloud:4", time.Now(), time.Now().Add(time.Hour))
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "/tv.plex.providers.epg.cloud:4") {
		t.Errorf("error should include URL; got: %v", err)
	}
}

// --- Cross-cutting: every request sends X-Plex-Token ---

func TestClient_AlwaysSendsToken(t *testing.T) {
	calls := 0
	srv := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-Plex-Token") != "secret-token" {
			t.Errorf("request %d missing X-Plex-Token: %q", calls, r.Header.Get("X-Plex-Token"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/identity":
			_, _ = w.Write([]byte(`{"MediaContainer":{}}`))
		case r.URL.Path == "/livetv/dvrs":
			_, _ = w.Write([]byte(`{"MediaContainer":{"Dvr":[]}}`))
		case strings.Contains(r.URL.Path, "/channels"):
			_, _ = w.Write([]byte(`{"MediaContainer":{"Channel":[]}}`))
		case strings.Contains(r.URL.Path, "/grid"):
			_, _ = w.Write([]byte(`{"MediaContainer":{"Metadata":[]}}`))
		}
	}))

	c := newClient(t, srv.URL, "secret-token")
	ctx := context.Background()
	_ = c.Ping(ctx)
	_, _ = c.GetDVRs(ctx)
	_, _ = c.GetLineupChannels(ctx, "tv.plex.providers.epg.cloud:4")
	_, _ = c.GetGrid(ctx, "tv.plex.providers.epg.cloud:4", time.Now(), time.Now().Add(time.Hour))

	if calls != 4 {
		t.Errorf("expected 4 calls, got %d", calls)
	}
}
