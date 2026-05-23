package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/acbgbca/xmltvguide/internal/api"
	"github.com/acbgbca/xmltvguide/internal/database"
	"github.com/acbgbca/xmltvguide/internal/images"
	"github.com/acbgbca/xmltvguide/internal/xmltv"
)

// newDeepCheckServer creates a test API server with seeded data and a stub
// XMLTV upstream. xmltvURL is the upstream URL the deepcheck probe targets.
func newDeepCheckServer(t *testing.T, xmltvURL string) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	imgDir := filepath.Join(dir, "images")
	if err := os.MkdirAll(filepath.Join(imgDir, "channels"), 0o750); err != nil {
		t.Fatalf("mkdir channels: %v", err)
	}
	db, err := database.Open(dbPath, 7, xmltvURL, images.NewCache(&http.Client{}, imgDir), nil, nil, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Seed some data so data_presence passes.
	base := time.Now().UTC().Truncate(24 * time.Hour)
	tv := &xmltv.TV{
		Channels: []xmltv.Channel{
			{ID: "ch1", DisplayNames: []xmltv.Name{{Value: "ABC"}}},
		},
		Programmes: []xmltv.Programme{
			{
				Start:   xmltv.XmltvTime{Time: base.Add(6 * time.Hour)},
				Stop:    xmltv.XmltvTime{Time: base.Add(7 * time.Hour)},
				Channel: "ch1",
				Titles:  []xmltv.Name{{Value: "Morning News"}},
			},
		},
	}
	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	mux := http.NewServeMux()
	handler := api.New(db, 0, nil, api.DeepCheckConfig{
		HTTPClient:    &http.Client{Timeout: 10 * time.Second},
		XMLTVURL:      xmltvURL,
		PollInterval:  12 * time.Hour,
		DBPath:        dbPath,
		ImageCacheDir: imgDir,
	})
	handler.RegisterRoutes(mux)

	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})
	return srv
}

func TestDeepCheck_HappyPath_Returns200AndSuccess(t *testing.T) {
	// Spin up a reachable XMLTV source that responds 200 to HEAD.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	srv := newDeepCheckServer(t, upstream.URL)

	resp, err := httpGet(t, srv.URL+"/api/deepcheck")
	if err != nil {
		t.Fatalf("GET /api/deepcheck: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body struct {
		Status string `json:"status"`
		Checks []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Error  string `json:"error,omitempty"`
			Info   string `json:"info,omitempty"`
		} `json:"checks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "SUCCESS" {
		t.Errorf("global status = %q, want SUCCESS; body=%+v", body.Status, body)
	}

	wantOrder := []string{
		"database",
		"database_writable",
		"fts",
		"data_presence",
		"data_freshness",
		"xmltv_url",
		"disk_data",
		"disk_tmp",
		"image_cache",
		"plex_reachable",
	}
	if len(body.Checks) != len(wantOrder) {
		t.Fatalf("got %d checks, want %d (%+v)", len(body.Checks), len(wantOrder), body.Checks)
	}
	for i, want := range wantOrder {
		if body.Checks[i].Name != want {
			t.Errorf("checks[%d].Name = %q, want %q", i, body.Checks[i].Name, want)
		}
		if body.Checks[i].Status != "SUCCESS" {
			t.Errorf("checks[%d] %s status = %q, want SUCCESS (error=%q)",
				i, body.Checks[i].Name, body.Checks[i].Status, body.Checks[i].Error)
		}
	}
}

func TestDeepCheck_UnreachableXMLTV_Returns503(t *testing.T) {
	// Point at an unreachable URL (TCP port 1 is reserved).
	srv := newDeepCheckServer(t, "http://127.0.0.1:1/")

	resp, err := httpGet(t, srv.URL+"/api/deepcheck")
	if err != nil {
		t.Fatalf("GET /api/deepcheck: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", resp.StatusCode)
	}

	var body struct {
		Status string `json:"status"`
		Checks []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Error  string `json:"error,omitempty"`
		} `json:"checks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "FAILURE" {
		t.Errorf("global status = %q, want FAILURE", body.Status)
	}

	// All checks should be present even when xmltv_url fails.
	if len(body.Checks) != 10 {
		t.Errorf("expected 10 checks, got %d", len(body.Checks))
	}
	var xmltv *struct {
		Name, Status, Error string
	}
	for _, c := range body.Checks {
		if c.Name == "xmltv_url" {
			xmltv = &struct{ Name, Status, Error string }{c.Name, c.Status, c.Error}
			break
		}
	}
	if xmltv == nil {
		t.Fatalf("xmltv_url check not found in response")
	}
	if xmltv.Status != "FAILURE" {
		t.Errorf("xmltv_url status = %q, want FAILURE", xmltv.Status)
	}
}
