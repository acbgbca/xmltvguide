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
	"github.com/acbgbca/xmltvguide/internal/images"
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
	t.Cleanup(cs.Close)
	return cs
}

func populateDB(t *testing.T, db *database.DB, client *http.Client, xmltvURL string) {
	t.Helper()
	tv, err := xmltv.Fetch(context.Background(), client, xmltvURL)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
}

func setupStartupTest(t *testing.T) (*countingServer, *database.DB, string) {
	t.Helper()
	xmlBytes, err := os.ReadFile("testdata/sample.xml")
	if err != nil {
		t.Fatalf("read sample.xml: %v", err)
	}
	mockSrv := startMockXMLTVServer(t, string(xmlBytes))

	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, mockSrv.URL+"/xmltv", images.NewCache(&http.Client{}, filepath.Join(dir, "images")), nil, nil, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	populateDB(t, db, &http.Client{}, mockSrv.URL+"/xmltv")
	mockSrv.resetRequests()

	return mockSrv, db, mockSrv.URL + "/xmltv"
}

func newIntegrationServer(t *testing.T, xmltvURL string) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, xmltvURL, images.NewCache(&http.Client{}, filepath.Join(dir, "images")), nil, nil, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	populateDB(t, db, &http.Client{}, xmltvURL+"/xmltv")

	mux := http.NewServeMux()
	handler := api.New(db, 0)
	handler.RegisterRoutes(mux)

	webSub, err := fs.Sub(webFS, "web")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	mux.Handle("/", spaHandler(http.FS(webSub)))

	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})
	return srv
}

func newSampleIntegrationServer(t *testing.T) *httptest.Server {
	t.Helper()
	xmlBytes, err := os.ReadFile("testdata/sample.xml")
	if err != nil {
		t.Fatalf("read sample.xml: %v", err)
	}
	mockSrv := startMockXMLTVServer(t, string(xmlBytes))
	return newIntegrationServer(t, mockSrv.URL)
}

// httpGet performs a GET request with a background context.
// It is a context-aware replacement for http.Get in tests.
func httpGet(t *testing.T, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}

func fetchBody(t *testing.T, srv *httptest.Server, path string) string {
	t.Helper()
	resp, err := httpGet(t, srv.URL+path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: expected 200, got %d", path, resp.StatusCode)
	}
	var buf strings.Builder
	io.Copy(&buf, resp.Body) //nolint:errcheck
	return buf.String()
}

func TestIntegration_StaticFiles(t *testing.T) {
	srv := newSampleIntegrationServer(t)

	t.Run("index_html", func(t *testing.T) {
		resp, err := httpGet(t, srv.URL+"/")
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

	t.Run("app_js_removed", func(t *testing.T) {
		resp, err := httpGet(t, srv.URL+"/app.js")
		if err != nil {
			t.Fatalf("GET /app.js: %v", err)
		}
		defer resp.Body.Close()
		// The SPA handler falls back to index.html for missing files, so the
		// response is 200 but with text/html — not a JavaScript file.
		ct := resp.Header.Get("Content-Type")
		if strings.Contains(ct, "javascript") {
			t.Error("/app.js should no longer serve JavaScript (file should be removed)")
		}
	})

	t.Run("main_js", func(t *testing.T) {
		resp, err := httpGet(t, srv.URL+"/js/main.js")
		if err != nil {
			t.Fatalf("GET /js/main.js: %v", err)
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

	t.Run("index_uses_module_script", func(t *testing.T) {
		resp, err := httpGet(t, srv.URL+"/")
		if err != nil {
			t.Fatalf("GET /: %v", err)
		}
		defer resp.Body.Close()
		var buf strings.Builder
		io.Copy(&buf, resp.Body) //nolint:errcheck
		body := buf.String()
		if !strings.Contains(body, `type="module"`) {
			t.Error("index.html script tag should have type=\"module\"")
		}
		if !strings.Contains(body, `/js/main.js`) {
			t.Error("index.html should reference /js/main.js")
		}
		if strings.Contains(body, `/app.js`) {
			t.Error("index.html should not reference /app.js")
		}
	})

	t.Run("sw_caches_main_js", func(t *testing.T) {
		resp, err := httpGet(t, srv.URL+"/sw.js")
		if err != nil {
			t.Fatalf("GET /sw.js: %v", err)
		}
		defer resp.Body.Close()
		var buf strings.Builder
		io.Copy(&buf, resp.Body) //nolint:errcheck
		body := buf.String()
		if !strings.Contains(body, `/js/main.js`) {
			t.Error("sw.js STATIC list should include /js/main.js")
		}
		if strings.Contains(body, `/app.js`) {
			t.Error("sw.js should not reference /app.js")
		}
	})

	t.Run("manifest_json", func(t *testing.T) {
		resp, err := httpGet(t, srv.URL+"/manifest.json")
		if err != nil {
			t.Fatalf("GET /manifest.json: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("manifest_includes_png_icons", func(t *testing.T) {
		resp, err := httpGet(t, srv.URL+"/manifest.json")
		if err != nil {
			t.Fatalf("GET /manifest.json: %v", err)
		}
		defer resp.Body.Close()
		var manifest struct {
			Icons []struct {
				Src   string `json:"src"`
				Sizes string `json:"sizes"`
				Type  string `json:"type"`
			} `json:"icons"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
			t.Fatalf("decode manifest: %v", err)
		}
		// Must include at least a 192x192 and 512x512 PNG icon
		foundSizes := map[string]bool{}
		for _, icon := range manifest.Icons {
			if icon.Type == "image/png" {
				foundSizes[icon.Sizes] = true
			}
		}
		for _, size := range []string{"192x192", "512x512"} {
			if !foundSizes[size] {
				t.Errorf("manifest missing PNG icon with size %s", size)
			}
		}
	})

	t.Run("apple_touch_icon", func(t *testing.T) {
		resp, err := httpGet(t, srv.URL+"/apple-touch-icon.png")
		if err != nil {
			t.Fatalf("GET /apple-touch-icon.png: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "image/png") {
			t.Errorf("Content-Type: expected image/png, got %q", ct)
		}
	})

	t.Run("index_has_apple_touch_icon_link", func(t *testing.T) {
		resp, err := httpGet(t, srv.URL+"/")
		if err != nil {
			t.Fatalf("GET /: %v", err)
		}
		defer resp.Body.Close()
		var buf strings.Builder
		io.Copy(&buf, resp.Body) //nolint:errcheck
		body := buf.String()
		if !strings.Contains(body, `rel="apple-touch-icon"`) {
			t.Error("index.html missing apple-touch-icon link")
		}
	})

	t.Run("sw_caches_icon_files", func(t *testing.T) {
		resp, err := httpGet(t, srv.URL+"/sw.js")
		if err != nil {
			t.Fatalf("GET /sw.js: %v", err)
		}
		defer resp.Body.Close()
		var buf strings.Builder
		io.Copy(&buf, resp.Body) //nolint:errcheck
		body := buf.String()
		for _, icon := range []string{"/icon.svg", "/apple-touch-icon.png"} {
			if !strings.Contains(body, icon) {
				t.Errorf("sw.js STATIC list missing %s", icon)
			}
		}
	})
}

func TestIntegration_Channels(t *testing.T) {
	srv := newSampleIntegrationServer(t)

	resp, err := httpGet(t, srv.URL+"/api/channels")
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

// sampleXMLTVForDate generates XMLTV XML with the same shape as testdata/sample.xml
// but with all programme times anchored to baseDate (midnight UTC). Using a
// dynamic date prevents the "Late Night Movie" airing from ageing out of the
// 7-day retention window as time passes.
func sampleXMLTVForDate(baseDate time.Time) string {
	const layout = "20060102150405 +0000"
	prevEvening := baseDate.Add(-time.Hour) // 23:00 the previous day
	earlyMorning := baseDate.Add(time.Hour) // 01:00 today
	morning6 := baseDate.Add(6 * time.Hour)
	morning7 := baseDate.Add(7 * time.Hour)
	morning630 := baseDate.Add(6*time.Hour + 30*time.Minute)
	morning730 := baseDate.Add(7*time.Hour + 30*time.Minute)
	morning9 := baseDate.Add(9 * time.Hour)
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE tv SYSTEM "xmltv.dtd">
<tv>
  <channel id="ch1">
    <display-name>ABC</display-name>
    <lcn>2</lcn>
    <icon src="https://example.com/abc.png"/>
  </channel>
  <channel id="ch2">
    <display-name>SBS</display-name>
  </channel>
  <programme start="%s" stop="%s" channel="ch2">
    <title>Late Night Movie</title>
    <desc>A classic late night movie spanning midnight.</desc>
    <category>Movie</category>
  </programme>
  <programme start="%s" stop="%s" channel="ch1">
    <title>Morning News</title>
    <desc>The latest news to start your day.</desc>
    <category>News</category>
    <rating system="ABA"><value>G</value></rating>
  </programme>
  <programme start="%s" stop="%s" channel="ch1">
    <title>Sunrise</title>
    <sub-title>Monday Edition</sub-title>
    <desc>Morning breakfast television programme.</desc>
    <category>Entertainment</category>
    <episode-num system="xmltv_ns">5.12.0/1</episode-num>
    <episode-num system="onscreen">S06 E13</episode-num>
    <star-rating system="imdb"><value>3.5/5</value></star-rating>
    <previously-shown/>
  </programme>
  <programme start="%s" stop="%s" channel="ch2">
    <title>World News</title>
    <sub-title>International Edition</sub-title>
    <desc>Comprehensive international news coverage.</desc>
    <category>News</category>
    <category>International</category>
    <date>2026</date>
    <premiere>First broadcast</premiere>
  </programme>
</tv>`,
		prevEvening.Format(layout), earlyMorning.Format(layout),
		morning6.Format(layout), morning7.Format(layout),
		morning7.Format(layout), morning9.Format(layout),
		morning630.Format(layout), morning730.Format(layout),
	)
}

func TestIntegration_Guide(t *testing.T) {
	base := time.Now().UTC().Truncate(24 * time.Hour)
	mockSrv := startMockXMLTVServer(t, sampleXMLTVForDate(base))

	srv := newIntegrationServer(t, mockSrv.URL)

	dateStr := base.Format("2006-01-02")
	resp, err := httpGet(t, srv.URL+"/api/guide?date="+dateStr)
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
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, mockSrv.URL+"/xmltv", images.NewCache(&http.Client{}, filepath.Join(dir, "images")), nil, nil, nil)
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
	if !db.HasData(context.Background()) {
		t.Error("expected database to contain data after initial fetch")
	}
}

func TestStartup_ExistingData_SkipsFetch(t *testing.T) {
	mockSrv, db, xmltvURL := setupStartupTest(t)

	// Simulate a restart with data present and refreshOnStart=false (the default).
	runInitialRefresh(db, &http.Client{}, xmltvURL, time.Hour, false)

	if count := mockSrv.requestCount(); count != 0 {
		t.Errorf("expected 0 XMLTV requests when data exists and REFRESH_ON_START=false, got %d", count)
	}
}

func TestStartup_RefreshOnStart_FetchesEvenWithData(t *testing.T) {
	mockSrv, db, xmltvURL := setupStartupTest(t)

	// REFRESH_ON_START=true must always fetch, even when data is already present.
	runInitialRefresh(db, &http.Client{}, xmltvURL, time.Hour, true)

	if count := mockSrv.requestCount(); count != 1 {
		t.Errorf("expected 1 XMLTV request with REFRESH_ON_START=true, got %d", count)
	}
}

func TestIntegration_Status(t *testing.T) {
	srv := newSampleIntegrationServer(t)

	resp, err := httpGet(t, srv.URL+"/api/status")
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
	if result.SourceUrl == "" {
		t.Error("sourceUrl should be non-empty")
	}
}

// TestIntegration_ChannelIconProxy verifies the full icon proxy flow end-to-end:
// refresh downloads the icon, /api/channels returns the proxy URL, and
// GET /images/channel/{id} serves the image content.
// TestIntegration_Navigation_ButtonsEnabled verifies that the prevDay and
// nextDay buttons are present in the served HTML and are not disabled — a
// pre-condition for the multi-day navigation feature to work.
func TestIntegration_Navigation_ButtonsEnabled(t *testing.T) {
	srv := newSampleIntegrationServer(t)

	resp, err := httpGet(t, srv.URL+"/")
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

// TestIntegration_Navigation_JSFunctions verifies that the guide page module
// and main.js together export the functions required for multi-day navigation.
func TestIntegration_Navigation_JSFunctions(t *testing.T) {
	srv := newSampleIntegrationServer(t)

	// Check main.js references the guide-related functions (via imports)
	resp, err := httpGet(t, srv.URL+"/js/main.js")
	if err != nil {
		t.Fatalf("GET /js/main.js: %v", err)
	}
	defer resp.Body.Close()

	var buf strings.Builder
	io.Copy(&buf, resp.Body) //nolint:errcheck
	body := buf.String()

	for _, fn := range []string{"getDateFromURL", "navigateToDate", "addDays"} {
		if !strings.Contains(body, fn) {
			t.Errorf("expected js/main.js to reference %s", fn)
		}
	}

	// Check pages/guide.js defines the guide-specific URL and navigation functions
	resp2, err := httpGet(t, srv.URL+"/js/pages/guide.js")
	if err != nil {
		t.Fatalf("GET /js/pages/guide.js: %v", err)
	}
	defer resp2.Body.Close()

	var buf2 strings.Builder
	io.Copy(&buf2, resp2.Body) //nolint:errcheck
	guideBody := buf2.String()

	for _, fn := range []string{"getDateFromURL", "setDateInURL", "navigateToDate"} {
		if !strings.Contains(guideBody, fn) {
			t.Errorf("expected js/pages/guide.js to define %s", fn)
		}
	}
}

// TestIntegration_Search_JSFunctions verifies that the search page module
// and main.js together export the functions required for search functionality.
func TestIntegration_Search_JSFunctions(t *testing.T) {
	srv := newSampleIntegrationServer(t)

	// Check pages/search.js is served and defines the expected functions
	resp, err := httpGet(t, srv.URL+"/js/pages/search.js")
	if err != nil {
		t.Fatalf("GET /js/pages/search.js: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /js/pages/search.js: expected 200, got %d", resp.StatusCode)
	}

	var buf strings.Builder
	io.Copy(&buf, resp.Body) //nolint:errcheck
	searchBody := buf.String()

	for _, fn := range []string{"setupSearchPage", "triggerSearch", "renderCategoryChips", "getCurrentSearchConfig"} {
		if !strings.Contains(searchBody, fn) {
			t.Errorf("expected js/pages/search.js to define %s", fn)
		}
	}

	// Check main.js imports from search.js and no longer defines these functions directly
	resp2, err := httpGet(t, srv.URL+"/js/main.js")
	if err != nil {
		t.Fatalf("GET /js/main.js: %v", err)
	}
	defer resp2.Body.Close()

	var buf2 strings.Builder
	io.Copy(&buf2, resp2.Body) //nolint:errcheck
	mainBody := buf2.String()

	if !strings.Contains(mainBody, "pages/search.js") {
		t.Error("expected js/main.js to import from pages/search.js")
	}

	// Check that search.js is listed in the service worker cache
	resp3, err := httpGet(t, srv.URL+"/sw.js")
	if err != nil {
		t.Fatalf("GET /sw.js: %v", err)
	}
	defer resp3.Body.Close()

	var buf3 strings.Builder
	io.Copy(&buf3, resp3.Body) //nolint:errcheck
	swBody := buf3.String()

	if !strings.Contains(swBody, "pages/search.js") {
		t.Error("expected sw.js to include js/pages/search.js in the cache list")
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
		images.NewCache(mockSrv.Client(), filepath.Join(dir, "images")),
		nil, nil, nil,
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
	api.New(db, 0).RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// /api/channels must return proxy URL for ch1, empty icon for ch2.
	resp, err := httpGet(t, srv.URL+"/api/channels")
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
	iconResp, err := httpGet(t, srv.URL+"/images/channel/ch1")
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
	noIconResp, err := httpGet(t, srv.URL+"/images/channel/ch2")
	if err != nil {
		t.Fatalf("GET /images/channel/ch2: %v", err)
	}
	noIconResp.Body.Close()
	if noIconResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for ch2 (no icon), got %d", noIconResp.StatusCode)
	}
}

// TestIntegration_Search verifies the full search flow: load XMLTV data,
// then call /api/search and verify the response shape.
func TestIntegration_Search(t *testing.T) {
	base := time.Now().UTC().Truncate(24 * time.Hour)
	mockSrv := startMockXMLTVServer(t, sampleXMLTVForDate(base))
	srv := newIntegrationServer(t, mockSrv.URL)

	// Search for "News" — sample.xml has "Morning News" and "World News"
	resp, err := httpGet(t, srv.URL+"/api/search?q=News&mode=advanced&include_past=true")
	if err != nil {
		t.Fatalf("GET /api/search: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: expected application/json, got %q", ct)
	}

	var result []struct {
		Title   string `json:"title"`
		Airings []struct {
			ChannelID   string `json:"channelId"`
			ChannelName string `json:"channelName"`
		} `json:"airings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(result) == 0 {
		t.Fatal("expected at least one search result group")
	}

	// Verify channelName is populated
	for _, g := range result {
		for _, a := range g.Airings {
			if a.ChannelName == "" {
				t.Errorf("channelName should be populated for airing in group %q", g.Title)
			}
		}
	}
}

// TestIntegration_Categories verifies the /api/categories endpoint returns
// categories from the loaded XMLTV data.
func TestIntegration_Categories(t *testing.T) {
	base := time.Now().UTC().Truncate(24 * time.Hour)
	mockSrv := startMockXMLTVServer(t, sampleXMLTVForDate(base))
	srv := newIntegrationServer(t, mockSrv.URL)

	resp, err := httpGet(t, srv.URL+"/api/categories")
	if err != nil {
		t.Fatalf("GET /api/categories: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result []string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// sample.xml has categories: Movie, News, Entertainment, International
	if len(result) == 0 {
		t.Fatal("expected at least one category")
	}

	// Verify sorted
	for i := 1; i < len(result); i++ {
		if result[i] < result[i-1] {
			t.Errorf("categories not sorted: %q before %q", result[i-1], result[i])
		}
	}
}

// TestIntegration_Search_MissingQuery verifies 400 for missing q parameter.
func TestIntegration_Search_MissingQuery(t *testing.T) {
	srv := newSampleIntegrationServer(t)

	resp, err := httpGet(t, srv.URL+"/api/search")
	if err != nil {
		t.Fatalf("GET /api/search: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// TestIntegration_SPAFallback verifies that SPA routes return index.html (HTTP 200, text/html).
func TestIntegration_SPAFallback(t *testing.T) {
	srv := newSampleIntegrationServer(t)

	cases := []struct {
		name      string
		path      string
		checkBody bool
	}{
		{name: "search", path: "/search", checkBody: true},
		{name: "favourites", path: "/favourites", checkBody: true},
		{name: "settings", path: "/settings", checkBody: false},
		{name: "guide", path: "/guide?date=2026-04-01", checkBody: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := httpGet(t, srv.URL+tc.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tc.path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}
			ct := resp.Header.Get("Content-Type")
			if !strings.Contains(ct, "text/html") {
				t.Errorf("Content-Type: expected text/html, got %q", ct)
			}
			if tc.checkBody {
				var buf strings.Builder
				io.Copy(&buf, resp.Body) //nolint:errcheck
				if !strings.Contains(buf.String(), "TV Guide") {
					t.Errorf("expected body to contain 'TV Guide'")
				}
			}
		})
	}
}

// TestIntegration_SearchPage_HTMLElements verifies that the search page
// contains the required UI elements (input, advanced options, results container).
func TestIntegration_SearchPage_HTMLElements(t *testing.T) {
	srv := newSampleIntegrationServer(t)

	resp, err := httpGet(t, srv.URL+"/search")
	if err != nil {
		t.Fatalf("GET /search: %v", err)
	}
	defer resp.Body.Close()

	var buf strings.Builder
	io.Copy(&buf, resp.Body) //nolint:errcheck
	body := buf.String()

	// Search input
	if !strings.Contains(body, `id="searchInput"`) {
		t.Error("expected search input with id='searchInput'")
	}
	if !strings.Contains(body, `Search programmes`) {
		t.Error("expected placeholder text 'Search programmes...'")
	}

	// Clear button
	if !strings.Contains(body, `id="searchClear"`) {
		t.Error("expected clear button with id='searchClear'")
	}

	// Advanced options toggle
	if !strings.Contains(body, `id="advancedToggle"`) {
		t.Error("expected advanced toggle with id='advancedToggle'")
	}

	// Advanced options panel
	if !strings.Contains(body, `id="advancedOptions"`) {
		t.Error("expected advanced options panel with id='advancedOptions'")
	}

	// Checkboxes
	if !strings.Contains(body, `id="searchDescriptions"`) {
		t.Error("expected 'Search descriptions' checkbox")
	}
	if !strings.Contains(body, `id="includePast"`) {
		t.Error("expected 'Include past airings' checkbox")
	}
	if !strings.Contains(body, `id="hideRepeats"`) {
		t.Error("expected 'Hide repeats' checkbox")
	}

	// Category chips container
	if !strings.Contains(body, `id="categoryChips"`) {
		t.Error("expected category chips container")
	}

	// Results container
	if !strings.Contains(body, `id="searchResults"`) {
		t.Error("expected search results container")
	}

	// Search hint
	if !strings.Contains(body, `id="searchHint"`) {
		t.Error("expected search hint element")
	}
}

// TestIntegration_SearchPage_JSFunctions verifies that the search page module
// and main.js together contain the functions required for the search UI.
func TestIntegration_SearchPage_JSFunctions(t *testing.T) {
	srv := newSampleIntegrationServer(t)

	searchJS := fetchBody(t, srv, "/js/pages/search.js")
	mainJS := fetchBody(t, srv, "/js/main.js")

	// Search-specific functions live in pages/search.js
	for _, fn := range []string{
		"performSearch",
		"fetchCategories",
		"renderSearchResults",
		"formatSearchDate",
	} {
		if !strings.Contains(searchJS, fn) {
			t.Errorf("expected js/pages/search.js to define %s", fn)
		}
	}

	// State additions are referenced in pages/search.js
	if !strings.Contains(searchJS, "state.categories") {
		t.Error("expected state.categories in js/pages/search.js")
	}
	if !strings.Contains(searchJS, "state.searchResults") {
		t.Error("expected state.searchResults in js/pages/search.js")
	}

	// main.js imports from search.js
	if !strings.Contains(mainJS, "pages/search.js") {
		t.Error("expected js/main.js to import from pages/search.js")
	}
}

// TestIntegration_SPAFallback_APINotCaught verifies that /api/channels
// still returns JSON and is not caught by the SPA fallback.
func TestIntegration_SPAFallback_APINotCaught(t *testing.T) {
	srv := newSampleIntegrationServer(t)

	resp, err := httpGet(t, srv.URL+"/api/channels")
	if err != nil {
		t.Fatalf("GET /api/channels: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: expected application/json, got %q", ct)
	}
}

// TestIntegration_SPAFallback_StaticFileNotCaught verifies that /style/base.css
// still returns the CSS file and is not caught by the SPA fallback.
func TestIntegration_SPAFallback_StaticFileNotCaught(t *testing.T) {
	srv := newSampleIntegrationServer(t)

	resp, err := httpGet(t, srv.URL+"/style/base.css")
	if err != nil {
		t.Fatalf("GET /style/base.css: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "css") {
		t.Errorf("Content-Type: expected css, got %q", ct)
	}
}

// TestIntegration_FavouritesPage_HTMLElements verifies that the favourites page
// contains the required UI elements for the saved-search favourites feature.
func TestIntegration_FavouritesPage_HTMLElements(t *testing.T) {
	srv := newSampleIntegrationServer(t)

	resp, err := httpGet(t, srv.URL+"/favourites")
	if err != nil {
		t.Fatalf("GET /favourites: %v", err)
	}
	defer resp.Body.Close()

	var buf strings.Builder
	io.Copy(&buf, resp.Body) //nolint:errcheck
	body := buf.String()

	// Favourites page container
	if !strings.Contains(body, `id="page-favourites"`) {
		t.Error("expected favourites page with id='page-favourites'")
	}

	// Favourites list container (where results are rendered)
	if !strings.Contains(body, `id="favouritesList"`) {
		t.Error("expected favourites list container with id='favouritesList'")
	}

	// Loading spinner for favourites
	if !strings.Contains(body, `id="favouritesLoading"`) {
		t.Error("expected favourites loading indicator with id='favouritesLoading'")
	}

	// Empty state message
	if !strings.Contains(body, `id="favouritesEmpty"`) {
		t.Error("expected favourites empty state with id='favouritesEmpty'")
	}

	// Should NOT contain the old placeholder text
	if strings.Contains(body, "Favourites coming soon") {
		t.Error("expected old placeholder 'Favourites coming soon' to be removed")
	}
}

// TestIntegration_FavouritesPage_JSFunctions verifies that the favourites
// feature functions are present across js/main.js and js/store/favourites.js.
func TestIntegration_FavouritesPage_JSFunctions(t *testing.T) {
	srv := newSampleIntegrationServer(t)

	mainJS := fetchBody(t, srv, "/js/main.js")
	favouritesJS := fetchBody(t, srv, "/js/pages/favourites.js")
	storeJS := fetchBody(t, srv, "/js/store/favourites.js")

	// Rendering and orchestration functions live in pages/favourites.js
	for _, fn := range []string{
		"renderFavouritesPage",
		"executeFavouriteSearches",
	} {
		if !strings.Contains(favouritesJS, fn) {
			t.Errorf("expected js/pages/favourites.js to define %s", fn)
		}
		// And should NOT be defined directly in main.js (only referenced via import)
		if strings.Contains(mainJS, "function "+fn) {
			t.Errorf("expected js/main.js NOT to define %s directly (should be imported)", fn)
		}
	}

	// editFavouriteSearch has moved to pages/search.js (alongside router import)
	searchJS := fetchBody(t, srv, "/js/pages/search.js")
	if !strings.Contains(searchJS, "editFavouriteSearch") {
		t.Error("expected editFavouriteSearch to be defined in js/pages/search.js")
	}
	if strings.Contains(mainJS, "function editFavouriteSearch") {
		t.Error("expected editFavouriteSearch NOT to be defined directly in js/main.js")
	}

	// router.js now imports renderFavouritesPage from pages/favourites.js
	routerJS := fetchBody(t, srv, "/js/router.js")
	if !strings.Contains(routerJS, "pages/favourites.js") {
		t.Error("expected js/router.js to import from pages/favourites.js")
	}

	// State references live in pages/favourites.js
	if !strings.Contains(favouritesJS, "state.favouriteSearches") {
		t.Error("expected state.favouriteSearches in js/pages/favourites.js")
	}
	if !strings.Contains(favouritesJS, "state.favouriteResults") {
		t.Error("expected state.favouriteResults in js/pages/favourites.js")
	}

	// Store functions and localStorage key live in js/store/favourites.js
	for _, fn := range []string{
		"loadFavouriteSearches",
		"saveFavouriteSearches",
		"addFavouriteSearch",
		"removeFavouriteSearch",
	} {
		if !strings.Contains(storeJS, fn) {
			t.Errorf("expected js/store/favourites.js to define %s", fn)
		}
	}
	if !strings.Contains(storeJS, "tvguide-favourites") {
		t.Error("expected 'tvguide-favourites' localStorage key in js/store/favourites.js")
	}
}

// TestIntegration_FavouritesPage_Module verifies that pages/favourites.js is
// served, exports renderFavouritesPage, and is registered in the service worker.
func TestIntegration_FavouritesPage_Module(t *testing.T) {
	srv := newSampleIntegrationServer(t)

	favouritesJS := fetchBody(t, srv, "/js/pages/favourites.js")

	// The module exports the page entry point
	if !strings.Contains(favouritesJS, "renderFavouritesPage") {
		t.Error("expected js/pages/favourites.js to export renderFavouritesPage")
	}

	// pages/favourites.js is in the service worker pre-cache list
	swJS := fetchBody(t, srv, "/sw.js")
	if !strings.Contains(swJS, "pages/favourites.js") {
		t.Error("expected sw.js to include js/pages/favourites.js in the cache list")
	}
}

// TestIntegration_ErrorLogging_JSFunctions verifies that the frontend error
// logging plumbing is in place: logError exported from api.js, global handlers
// registered in main.js, and all catch sites wired to logError.
func TestIntegration_ErrorLogging_JSFunctions(t *testing.T) {
	srv := newSampleIntegrationServer(t)

	apiJS := fetchBody(t, srv, "/js/api.js")
	mainJS := fetchBody(t, srv, "/js/main.js")

	// api.js must export logError
	if !strings.Contains(apiJS, "export function logError") {
		t.Error("expected api.js to export logError")
	}
	if !strings.Contains(apiJS, "/api/debug/log") {
		t.Error("expected api.js logError to POST to /api/debug/log")
	}

	// main.js must import logError from api.js
	if !strings.Contains(mainJS, "logError") {
		t.Error("expected main.js to import and use logError")
	}

	// Global error handlers must be registered
	if !strings.Contains(mainJS, "window.onerror") {
		t.Error("expected main.js to register window.onerror handler")
	}
	if !strings.Contains(mainJS, "unhandledrejection") {
		t.Error("expected main.js to register unhandledrejection handler")
	}

	searchJS := fetchBody(t, srv, "/js/pages/search.js")
	favouritesJS := fetchBody(t, srv, "/js/pages/favourites.js")

	// All explicit catch sites must call logError with type: 'explicit'.
	// guide load in main.js; search fetch + categories in search.js;
	// favourite search in pages/favourites.js.
	totalExplicit := strings.Count(mainJS, "type: 'explicit'") +
		strings.Count(searchJS, "type: 'explicit'") +
		strings.Count(favouritesJS, "type: 'explicit'")
	if totalExplicit < 4 {
		t.Errorf("expected at least 4 explicit logError calls across main.js, search.js, and favourites.js (guide load, search, favourite search, categories), got %d", totalExplicit)
	}
}

// TestIntegration_CSSModularization_FilesServed verifies that each modular CSS
// file under web/style/ is served correctly and returns CSS content.
func TestIntegration_CSSModularization_FilesServed(t *testing.T) {
	srv := newSampleIntegrationServer(t)

	cssFiles := []string{
		"/style/base.css",
		"/style/layout.css",
		"/style/guide.css",
		"/style/modal.css",
		"/style/search.css",
		"/style/favourites.css",
		"/style/settings.css",
	}
	for _, path := range cssFiles {
		resp, err := httpGet(t, srv.URL+path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s: expected 200, got %d", path, resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "css") {
			t.Errorf("GET %s: expected css content-type, got %q", path, ct)
		}
	}
}

// TestIntegration_CSSModularization_IndexLoadsModularCSS verifies that
// index.html references the seven modular CSS files and not the old style.css.
func TestIntegration_CSSModularization_IndexLoadsModularCSS(t *testing.T) {
	srv := newSampleIntegrationServer(t)

	resp, err := httpGet(t, srv.URL+"/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	var buf strings.Builder
	io.Copy(&buf, resp.Body) //nolint:errcheck
	body := buf.String()

	cssFiles := []string{
		"/style/base.css",
		"/style/layout.css",
		"/style/guide.css",
		"/style/modal.css",
		"/style/search.css",
		"/style/favourites.css",
		"/style/settings.css",
	}
	for _, path := range cssFiles {
		if !strings.Contains(body, path) {
			t.Errorf("expected index.html to reference %s", path)
		}
	}

	if strings.Contains(body, `href="/style.css"`) {
		t.Error("expected index.html NOT to reference the old /style.css")
	}
}

// TestIntegration_CSSModularization_ServiceWorkerCachesAll verifies that
// sw.js includes all seven modular CSS files in its pre-cache list.
func TestIntegration_CSSModularization_ServiceWorkerCachesAll(t *testing.T) {
	srv := newSampleIntegrationServer(t)

	resp, err := httpGet(t, srv.URL+"/sw.js")
	if err != nil {
		t.Fatalf("GET /sw.js: %v", err)
	}
	defer resp.Body.Close()
	var buf strings.Builder
	io.Copy(&buf, resp.Body) //nolint:errcheck
	body := buf.String()

	cssFiles := []string{
		"style/base.css",
		"style/layout.css",
		"style/guide.css",
		"style/modal.css",
		"style/search.css",
		"style/favourites.css",
		"style/settings.css",
	}
	for _, path := range cssFiles {
		if !strings.Contains(body, path) {
			t.Errorf("expected sw.js to include %s in the cache list", path)
		}
	}

	if strings.Contains(body, "'/style.css'") {
		t.Error("expected sw.js NOT to cache the old /style.css")
	}
}

// TestIntegration_CSSModularization_BaseHasCSSVariables verifies that base.css
// contains the :root CSS custom properties used across all other files.
func TestIntegration_CSSModularization_BaseHasCSSVariables(t *testing.T) {
	srv := newSampleIntegrationServer(t)

	resp, err := httpGet(t, srv.URL+"/style/base.css")
	if err != nil {
		t.Fatalf("GET /style/base.css: %v", err)
	}
	defer resp.Body.Close()
	var buf strings.Builder
	io.Copy(&buf, resp.Body) //nolint:errcheck
	body := buf.String()

	if !strings.Contains(body, ":root") {
		t.Error("expected base.css to contain :root CSS custom properties")
	}
	for _, v := range []string{"--bg-primary", "--text-primary", "--row-height"} {
		if !strings.Contains(body, v) {
			t.Errorf("expected base.css to define CSS variable %s", v)
		}
	}
}

// TestIntegration_AuthRedirectHandling verifies that api.js and sw.js are
// configured to detect and handle Traefik/Authelia authentication redirects.
func TestIntegration_AuthRedirectHandling(t *testing.T) {
	srv := newSampleIntegrationServer(t)

	t.Run("api_js_uses_redirect_manual", func(t *testing.T) {
		resp, err := httpGet(t, srv.URL+"/js/api.js")
		if err != nil {
			t.Fatalf("GET /js/api.js: %v", err)
		}
		defer resp.Body.Close()
		var buf strings.Builder
		io.Copy(&buf, resp.Body) //nolint:errcheck
		body := buf.String()
		if !strings.Contains(body, "redirect: 'manual'") {
			t.Error("api.js should use redirect: 'manual' for API fetch calls to detect auth redirects")
		}
	})

	t.Run("api_js_handles_opaqueredirect", func(t *testing.T) {
		resp, err := httpGet(t, srv.URL+"/js/api.js")
		if err != nil {
			t.Fatalf("GET /js/api.js: %v", err)
		}
		defer resp.Body.Close()
		var buf strings.Builder
		io.Copy(&buf, resp.Body) //nolint:errcheck
		body := buf.String()
		if !strings.Contains(body, "opaqueredirect") {
			t.Error("api.js should check for opaqueredirect response type to handle auth redirects")
		}
		if !strings.Contains(body, "window.location") {
			t.Error("api.js should trigger a page navigation when an opaqueredirect is detected")
		}
	})

	t.Run("sw_js_rethrows_on_api_fetch_failure", func(t *testing.T) {
		resp, err := httpGet(t, srv.URL+"/sw.js")
		if err != nil {
			t.Fatalf("GET /sw.js: %v", err)
		}
		defer resp.Body.Close()
		var buf strings.Builder
		io.Copy(&buf, resp.Body) //nolint:errcheck
		body := buf.String()
		// The service worker should NOT silently fall back to the cache for API
		// requests. If the network fails (e.g. due to an auth redirect), the
		// error must propagate so the page can trigger re-authentication.
		if strings.Contains(body, "fetch(event.request).catch(() => caches.match(event.request))") {
			t.Error("sw.js must not fall back to stale cache on API fetch failure — re-throw so the page can handle auth redirects")
		}
	})
}
