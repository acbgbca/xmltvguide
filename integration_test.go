package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/acbgbca/xmltvguide/internal/api"
	"github.com/acbgbca/xmltvguide/internal/database"
	"github.com/acbgbca/xmltvguide/internal/xmltv"
)

func TestMain(m *testing.M) {
	time.Local = time.UTC
	os.Exit(m.Run())
}

// countingServer wraps httptest.Server and tracks the number of requests served.
type countingServer struct {
	*httptest.Server
	mu    sync.Mutex
	count int
}

func (s *countingServer) requestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

func (s *countingServer) resetRequests() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count = 0
}

// startMockXMLTVServer starts an in-process HTTP server that serves xmlContent
// for any request. It registers a cleanup to close the server when the test ends.
func startMockXMLTVServer(t *testing.T, xmlContent string) *countingServer {
	t.Helper()
	cs := &countingServer{}
	cs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cs.mu.Lock()
		cs.count++
		cs.mu.Unlock()
		w.Header().Set("Content-Type", "text/xml")
		fmt.Fprint(w, xmlContent)
	}))
	t.Cleanup(cs.Server.Close)
	return cs
}

func newIntegrationServer(t *testing.T, xmltvURL string) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, xmltvURL, filepath.Join(dir, "images"), &http.Client{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	tv, err := xmltv.Fetch(context.Background(), &http.Client{}, xmltvURL+"/xmltv")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	mux := http.NewServeMux()
	handler := api.New(db)
	handler.RegisterRoutes(mux)

	webSub, err := fs.Sub(webFS, "web")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(webSub)))

	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})
	return srv
}

func TestIntegration_StaticFiles(t *testing.T) {
	xmlBytes, err := os.ReadFile("testdata/sample.xml")
	if err != nil {
		t.Fatalf("read sample.xml: %v", err)
	}
	mockSrv := startMockXMLTVServer(t, string(xmlBytes))

	srv := newIntegrationServer(t, mockSrv.URL)

	t.Run("index_html", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/")
		if err != nil {
			t.Fatalf("GET /: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Errorf("Content-Type: expected text/html, got %q", ct)
		}
		var buf strings.Builder
		io.Copy(&buf, resp.Body) //nolint:errcheck
		body := buf.String()
		if !strings.Contains(body, "TV Guide") {
			t.Error("expected body to contain 'TV Guide'")
		}
	})

	t.Run("app_js", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/app.js")
		if err != nil {
			t.Fatalf("GET /app.js: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "javascript") {
			t.Errorf("Content-Type: expected javascript, got %q", ct)
		}
	})

	t.Run("manifest_json", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/manifest.json")
		if err != nil {
			t.Fatalf("GET /manifest.json: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})
}

func TestIntegration_Channels(t *testing.T) {
	xmlBytes, err := os.ReadFile("testdata/sample.xml")
	if err != nil {
		t.Fatalf("read sample.xml: %v", err)
	}
	mockSrv := startMockXMLTVServer(t, string(xmlBytes))

	srv := newIntegrationServer(t, mockSrv.URL)

	resp, err := http.Get(srv.URL + "/api/channels")
	if err != nil {
		t.Fatalf("GET /api/channels: %v", err)
	}
	defer resp.Body.Close()

	var result []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(result))
	}
}

func TestIntegration_Guide(t *testing.T) {
	xmlBytes, err := os.ReadFile("testdata/sample.xml")
	if err != nil {
		t.Fatalf("read sample.xml: %v", err)
	}
	mockSrv := startMockXMLTVServer(t, string(xmlBytes))

	srv := newIntegrationServer(t, mockSrv.URL)

	resp, err := http.Get(srv.URL + "/api/guide?date=2026-03-29")
	if err != nil {
		t.Fatalf("GET /api/guide: %v", err)
	}
	defer resp.Body.Close()

	var result []struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 4 {
		t.Fatalf("expected 4 airings, got %d", len(result))
	}
}

func TestStartup_FreshInstall_AlwaysFetches(t *testing.T) {
	xmlBytes, err := os.ReadFile("testdata/sample.xml")
	if err != nil {
		t.Fatalf("read sample.xml: %v", err)
	}
	mockSrv := startMockXMLTVServer(t, string(xmlBytes))

	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, mockSrv.URL+"/xmltv", filepath.Join(dir, "images"), &http.Client{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Empty database, refreshOnStart=false (the default). Fresh install should
	// always fetch regardless.
	runInitialRefresh(db, &http.Client{}, mockSrv.URL+"/xmltv", time.Hour, false)

	if count := mockSrv.requestCount(); count != 1 {
		t.Errorf("expected 1 XMLTV request on fresh install, got %d", count)
	}
	if !db.HasData() {
		t.Error("expected database to contain data after initial fetch")
	}
}

func TestStartup_ExistingData_SkipsFetch(t *testing.T) {
	xmlBytes, err := os.ReadFile("testdata/sample.xml")
	if err != nil {
		t.Fatalf("read sample.xml: %v", err)
	}
	mockSrv := startMockXMLTVServer(t, string(xmlBytes))

	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, mockSrv.URL+"/xmltv", filepath.Join(dir, "images"), &http.Client{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Pre-populate the database to simulate a prior successful run.
	tv, err := xmltv.Fetch(context.Background(), &http.Client{}, mockSrv.URL+"/xmltv")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// Reset the counter so we only count requests made by runInitialRefresh.
	mockSrv.resetRequests()

	// Simulate a restart with data present and refreshOnStart=false (the default).
	runInitialRefresh(db, &http.Client{}, mockSrv.URL+"/xmltv", time.Hour, false)

	if count := mockSrv.requestCount(); count != 0 {
		t.Errorf("expected 0 XMLTV requests when data exists and REFRESH_ON_START=false, got %d", count)
	}
}

func TestStartup_RefreshOnStart_FetchesEvenWithData(t *testing.T) {
	xmlBytes, err := os.ReadFile("testdata/sample.xml")
	if err != nil {
		t.Fatalf("read sample.xml: %v", err)
	}
	mockSrv := startMockXMLTVServer(t, string(xmlBytes))

	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, mockSrv.URL+"/xmltv", filepath.Join(dir, "images"), &http.Client{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Pre-populate the database.
	tv, err := xmltv.Fetch(context.Background(), &http.Client{}, mockSrv.URL+"/xmltv")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	mockSrv.resetRequests()

	// REFRESH_ON_START=true must always fetch, even when data is already present.
	runInitialRefresh(db, &http.Client{}, mockSrv.URL+"/xmltv", time.Hour, true)

	if count := mockSrv.requestCount(); count != 1 {
		t.Errorf("expected 1 XMLTV request with REFRESH_ON_START=true, got %d", count)
	}
}

func TestIntegration_Status(t *testing.T) {
	xmlBytes, err := os.ReadFile("testdata/sample.xml")
	if err != nil {
		t.Fatalf("read sample.xml: %v", err)
	}
	mockSrv := startMockXMLTVServer(t, string(xmlBytes))

	srv := newIntegrationServer(t, mockSrv.URL)

	resp, err := http.Get(srv.URL + "/api/status")
	if err != nil {
		t.Fatalf("GET /api/status: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		SourceUrl string `json:"sourceUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(result.SourceUrl, mockSrv.URL) {
		t.Errorf("sourceUrl: expected to contain %q, got %q", mockSrv.URL, result.SourceUrl)
	}
}

// TestIntegration_ChannelIconProxy verifies the full icon proxy flow end-to-end:
// refresh downloads the icon, /api/channels returns the proxy URL, and
// GET /images/channel/{id} serves the image content.
// TestIntegration_Navigation_ButtonsEnabled verifies that the prevDay and
// nextDay buttons are present in the served HTML and are not disabled — a
// pre-condition for the multi-day navigation feature to work.
func TestIntegration_Navigation_ButtonsEnabled(t *testing.T) {
	xmlBytes, err := os.ReadFile("testdata/sample.xml")
	if err != nil {
		t.Fatalf("read sample.xml: %v", err)
	}
	mockSrv := startMockXMLTVServer(t, string(xmlBytes))
	srv := newIntegrationServer(t, mockSrv.URL)

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	var buf strings.Builder
	io.Copy(&buf, resp.Body) //nolint:errcheck
	body := buf.String()

	if strings.Contains(body, `id="prevDay" title="Previous day" disabled`) {
		t.Error("prevDay button should not have the disabled attribute")
	}
	if strings.Contains(body, `id="nextDay" title="Next day" disabled`) {
		t.Error("nextDay button should not have the disabled attribute")
	}
}

// TestIntegration_Navigation_JSFunctions verifies that app.js exports the
// functions required for multi-day navigation.
func TestIntegration_Navigation_JSFunctions(t *testing.T) {
	xmlBytes, err := os.ReadFile("testdata/sample.xml")
	if err != nil {
		t.Fatalf("read sample.xml: %v", err)
	}
	mockSrv := startMockXMLTVServer(t, string(xmlBytes))
	srv := newIntegrationServer(t, mockSrv.URL)

	resp, err := http.Get(srv.URL + "/app.js")
	if err != nil {
		t.Fatalf("GET /app.js: %v", err)
	}
	defer resp.Body.Close()

	var buf strings.Builder
	io.Copy(&buf, resp.Body) //nolint:errcheck
	body := buf.String()

	for _, fn := range []string{"getDateFromURL", "setDateInURL", "navigateToDate", "addDays"} {
		if !strings.Contains(body, fn) {
			t.Errorf("expected app.js to define %s", fn)
		}
	}
}

func TestIntegration_ChannelIconProxy(t *testing.T) {
	// Mock server serves both XMLTV data and icon images.
	iconRequests := 0
	var mu sync.Mutex
	mockSrv := &countingServer{}
	mockSrv.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mockSrv.mu.Lock()
		mockSrv.count++
		mockSrv.mu.Unlock()

		if strings.HasSuffix(r.URL.Path, ".png") {
			mu.Lock()
			iconRequests++
			mu.Unlock()
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("\x89PNG\r\n\x1a\n"))
			return
		}
		// Serve XMLTV with icon pointing back to this server.
		w.Header().Set("Content-Type", "text/xml")
		fmt.Fprintf(w, `<?xml version="1.0"?>
<tv>
  <channel id="ch1">
    <display-name>ABC</display-name>
    <icon src="%s/abc.png"/>
  </channel>
  <channel id="ch2">
    <display-name>SBS</display-name>
  </channel>
</tv>`, mockSrv.URL)
	}))
	t.Cleanup(mockSrv.Close)

	dir := t.TempDir()
	db, err := database.Open(
		filepath.Join(dir, "test.db"), 7, mockSrv.URL+"/xmltv",
		filepath.Join(dir, "images"), mockSrv.Client(),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	tv, err := xmltv.Fetch(context.Background(), mockSrv.Client(), mockSrv.URL+"/xmltv")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	mux := http.NewServeMux()
	api.New(db).RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// /api/channels must return proxy URL for ch1, empty icon for ch2.
	resp, err := http.Get(srv.URL + "/api/channels")
	if err != nil {
		t.Fatalf("GET /api/channels: %v", err)
	}
	defer resp.Body.Close()

	var channels []struct {
		ID   string `json:"id"`
		Icon string `json:"icon"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&channels); err != nil {
		t.Fatalf("decode channels: %v", err)
	}

	var ch1Icon, ch2Icon string
	for _, ch := range channels {
		if ch.ID == "ch1" {
			ch1Icon = ch.Icon
		}
		if ch.ID == "ch2" {
			ch2Icon = ch.Icon
		}
	}
	if ch1Icon != "/images/channel/ch1" {
		t.Errorf("ch1 icon: expected %q, got %q", "/images/channel/ch1", ch1Icon)
	}
	if ch2Icon != "" {
		t.Errorf("ch2 icon: expected empty, got %q", ch2Icon)
	}

	// GET /images/channel/ch1 must serve the cached PNG.
	iconResp, err := http.Get(srv.URL + "/images/channel/ch1")
	if err != nil {
		t.Fatalf("GET /images/channel/ch1: %v", err)
	}
	defer iconResp.Body.Close()

	if iconResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", iconResp.StatusCode)
	}
	ct := iconResp.Header.Get("Content-Type")
	if !strings.Contains(ct, "image/png") {
		t.Errorf("Content-Type: expected image/png, got %q", ct)
	}

	// GET /images/channel/ch2 must return 404 (no icon).
	noIconResp, err := http.Get(srv.URL + "/images/channel/ch2")
	if err != nil {
		t.Fatalf("GET /images/channel/ch2: %v", err)
	}
	noIconResp.Body.Close()
	if noIconResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for ch2 (no icon), got %d", noIconResp.StatusCode)
	}
}
