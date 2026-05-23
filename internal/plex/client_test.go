package plex_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
	body := `{
		"MediaContainer": {
			"Dvr": [
				{"id": 1, "lineupIDs": ["xmltv:1"], "gridKey": "/tv.plex.providers.epg.xmltv:1"},
				{"id": 2, "lineupIDs": ["cloud:7", "cloud:8"], "gridKey": "/tv.plex.providers.epg.cloud:2"}
			]
		}
	}`
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
	if len(dvrs) != 2 {
		t.Fatalf("expected 2 DVRs, got %d", len(dvrs))
	}
	if dvrs[0].ID != 1 {
		t.Errorf("dvrs[0].ID = %d, want 1", dvrs[0].ID)
	}
	if dvrs[0].GridKey != "/tv.plex.providers.epg.xmltv:1" {
		t.Errorf("dvrs[0].GridKey = %q", dvrs[0].GridKey)
	}
	if len(dvrs[0].LineupIDs) != 1 || dvrs[0].LineupIDs[0] != "xmltv:1" {
		t.Errorf("dvrs[0].LineupIDs = %v", dvrs[0].LineupIDs)
	}
	if len(dvrs[1].LineupIDs) != 2 {
		t.Errorf("dvrs[1].LineupIDs = %v, want 2 entries", dvrs[1].LineupIDs)
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
	body := `{
		"MediaContainer": {
			"Channel": [
				{"id": "ch-1", "xmltvId": "abc.com.au", "lcn": "2", "title": "ABC"},
				{"id": "ch-2", "title": "SBS Viceland"}
			]
		}
	}`
	srv := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/epg/lineups/xmltv:1/channels" {
			t.Errorf("expected /epg/lineups/xmltv:1/channels, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))

	c := newClient(t, srv.URL, "tok")
	chans, err := c.GetLineupChannels(context.Background(), "xmltv:1")
	if err != nil {
		t.Fatalf("GetLineupChannels: %v", err)
	}
	if len(chans) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(chans))
	}
	if chans[0].ID != "ch-1" {
		t.Errorf("chans[0].ID = %q", chans[0].ID)
	}
	if chans[0].XMLTVID != "abc.com.au" {
		t.Errorf("chans[0].XMLTVID = %q", chans[0].XMLTVID)
	}
	if chans[0].LCN != "2" {
		t.Errorf("chans[0].LCN = %q", chans[0].LCN)
	}
	if chans[0].DisplayName != "ABC" {
		t.Errorf("chans[0].DisplayName = %q", chans[0].DisplayName)
	}
	if chans[1].XMLTVID != "" || chans[1].LCN != "" {
		t.Errorf("optional fields should be empty: %+v", chans[1])
	}
}

func TestGetLineupChannels_Unauthorized(t *testing.T) {
	srv := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	c := newClient(t, srv.URL, "tok")
	_, err := c.GetLineupChannels(context.Background(), "xmltv:1")
	if !errors.Is(err, plex.ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}

// --- GetGrid ---

func TestGetGrid_HappyPath(t *testing.T) {
	begins := time.Date(2025, 6, 10, 12, 0, 0, 0, time.UTC)
	ends := begins.Add(2 * time.Hour)

	body := `{
		"MediaContainer": {
			"Metadata": [
				{"ratingKey": "rk-1", "channel": "ch-1", "beginsAt": 1749556800, "endsAt": 1749560400, "title": "News at Noon", "ddProgID": "EP000123450001"},
				{"ratingKey": "rk-2", "channel": "ch-1", "beginsAt": 1749560400, "endsAt": 1749564000, "title": "Sports"}
			]
		}
	}`
	srv := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/tv.plex.providers.epg.xmltv:1/grid") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("type") != "4" {
			t.Errorf("type query = %q, want 4", q.Get("type"))
		}
		if q.Get("beginsAt") == "" {
			t.Errorf("beginsAt missing")
		}
		if q.Get("endsAt") == "" {
			t.Errorf("endsAt missing")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))

	c := newClient(t, srv.URL, "tok")
	entries, err := c.GetGrid(context.Background(), "/tv.plex.providers.epg.xmltv:1", begins, ends)
	if err != nil {
		t.Fatalf("GetGrid: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	if entries[0].RatingKey != "rk-1" {
		t.Errorf("entries[0].RatingKey = %q", entries[0].RatingKey)
	}
	if entries[0].Channel != "ch-1" {
		t.Errorf("entries[0].Channel = %q", entries[0].Channel)
	}
	if entries[0].BeginsAt != 1749556800 {
		t.Errorf("entries[0].BeginsAt = %d", entries[0].BeginsAt)
	}
	if entries[0].EndsAt != 1749560400 {
		t.Errorf("entries[0].EndsAt = %d", entries[0].EndsAt)
	}
	if entries[0].Title != "News at Noon" {
		t.Errorf("entries[0].Title = %q", entries[0].Title)
	}
	if entries[0].DdProgID != "EP000123450001" {
		t.Errorf("entries[0].DdProgID = %q", entries[0].DdProgID)
	}
	if entries[1].DdProgID != "" {
		t.Errorf("entries[1].DdProgID should be empty when absent; got %q", entries[1].DdProgID)
	}
}

func TestGetGrid_PassesUnixTimes(t *testing.T) {
	begins := time.Date(2025, 6, 10, 0, 0, 0, 0, time.UTC)
	ends := time.Date(2025, 6, 11, 0, 0, 0, 0, time.UTC)

	var capturedBegins, capturedEnds string
	srv := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBegins = r.URL.Query().Get("beginsAt")
		capturedEnds = r.URL.Query().Get("endsAt")
		_, _ = w.Write([]byte(`{"MediaContainer":{"Metadata":[]}}`))
	}))

	c := newClient(t, srv.URL, "tok")
	if _, err := c.GetGrid(context.Background(), "/grid-key", begins, ends); err != nil {
		t.Fatalf("GetGrid: %v", err)
	}

	wantBegins := "1749513600" // unix seconds for 2025-06-10 UTC
	wantEnds := "1749600000"
	if capturedBegins != wantBegins {
		t.Errorf("beginsAt = %q, want %q", capturedBegins, wantBegins)
	}
	if capturedEnds != wantEnds {
		t.Errorf("endsAt = %q, want %q", capturedEnds, wantEnds)
	}
}

func TestGetGrid_NetworkTimeoutWraps(t *testing.T) {
	// Server delays beyond the client timeout.
	srv := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	c := plex.NewClient(srv.URL, "tok", &http.Client{Timeout: 50 * time.Millisecond})

	_, err := c.GetGrid(context.Background(), "/grid-key", time.Now(), time.Now().Add(time.Hour))
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "/grid-key") {
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
	_, _ = c.GetLineupChannels(ctx, "lineup")
	_, _ = c.GetGrid(ctx, "/grid-key", time.Now(), time.Now().Add(time.Hour))

	if calls != 4 {
		t.Errorf("expected 4 calls, got %d", calls)
	}
}
