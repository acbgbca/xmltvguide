package api_test

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// startFakeIconServer returns a test server that serves a minimal PNG for any request.
func startFakeIconServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("\x89PNG\r\n\x1a\n"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newSeededServer creates a test API server backed by a real database seeded with
// two channels (no icons) and two programmes.
func newSeededServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test-source", images.NewCache(&http.Client{}, filepath.Join(dir, "images")), nil, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Use today's date so airings are never pruned by the retention window.
	base := time.Now().UTC().Truncate(24 * time.Hour)
	tv := &xmltv.TV{
		Channels: []xmltv.Channel{
			{
				ID:           "ch1",
				DisplayNames: []xmltv.Name{{Value: "ABC"}},
				LCN:          "2",
			},
			{
				ID:           "ch2",
				DisplayNames: []xmltv.Name{{Value: "SBS"}},
			},
		},
		Programmes: []xmltv.Programme{
			{
				Start:   xmltv.XmltvTime{Time: base.Add(6 * time.Hour)},
				Stop:    xmltv.XmltvTime{Time: base.Add(7 * time.Hour)},
				Channel: "ch1",
				Titles:  []xmltv.Name{{Value: "Morning News"}},
			},
			{
				Start:      xmltv.XmltvTime{Time: base.Add(7 * time.Hour)},
				Stop:       xmltv.XmltvTime{Time: base.Add(8 * time.Hour)},
				Channel:    "ch2",
				Titles:     []xmltv.Name{{Value: "World News"}},
				Categories: []xmltv.Name{{Value: "News"}},
			},
		},
	}

	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	mux := http.NewServeMux()
	handler := api.New(db, 0)
	handler.RegisterRoutes(mux)

	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})
	return srv
}

// newSeededServerWithIcons creates a test API server where ch1 has an icon served
// by iconSrv. Refresh is called so the icon is downloaded and cached.
func newSeededServerWithIcons(t *testing.T, iconSrv *httptest.Server) (*httptest.Server, *database.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test-source", images.NewCache(iconSrv.Client(), filepath.Join(dir, "images")), nil, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	tv := &xmltv.TV{
		Channels: []xmltv.Channel{
			{
				ID:           "ch1",
				DisplayNames: []xmltv.Name{{Value: "ABC"}},
				Icons:        []xmltv.Icon{{Src: iconSrv.URL + "/abc.png"}},
			},
			{
				ID:           "ch2",
				DisplayNames: []xmltv.Name{{Value: "SBS"}},
			},
		},
	}

	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	mux := http.NewServeMux()
	handler := api.New(db, 0)
	handler.RegisterRoutes(mux)

	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})
	return srv, db
}

func TestGetChannels_Count(t *testing.T) {
	srv := newSeededServer(t)
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

func TestGetChannels_Order(t *testing.T) {
	srv := newSeededServer(t)
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
	if len(result) == 0 {
		t.Fatal("expected channels, got none")
	}
	if result[0].ID != "ch1" {
		t.Errorf("expected first channel ID %q, got %q", "ch1", result[0].ID)
	}
}

func TestGetChannels_LCN(t *testing.T) {
	srv := newSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/channels")
	if err != nil {
		t.Fatalf("GET /api/channels: %v", err)
	}
	defer resp.Body.Close()

	var result []struct {
		ID  string `json:"id"`
		LCN *int   `json:"lcn"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) < 2 {
		t.Fatalf("expected at least 2 channels, got %d", len(result))
	}
	// ch1 was seeded with LCN=2
	var ch1, ch2 *struct {
		ID  string `json:"id"`
		LCN *int   `json:"lcn"`
	}
	for i := range result {
		if result[i].ID == "ch1" {
			ch1 = &result[i]
		}
		if result[i].ID == "ch2" {
			ch2 = &result[i]
		}
	}
	if ch1 == nil || ch1.LCN == nil || *ch1.LCN != 2 {
		t.Errorf("ch1 LCN: expected 2, got %v", ch1)
	}
	if ch2 == nil || ch2.LCN != nil {
		t.Errorf("ch2 LCN: expected nil, got %v", ch2.LCN)
	}
}

func TestGetChannels_ContentType(t *testing.T) {
	srv := newSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/channels")
	if err != nil {
		t.Fatalf("GET /api/channels: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: expected application/json, got %q", ct)
	}
}

func TestGetGuide_Date(t *testing.T) {
	srv := newSeededServer(t)
	today := time.Now().UTC().Format("2006-01-02")
	resp, err := http.Get(srv.URL + "/api/guide?date=" + today)
	if err != nil {
		t.Fatalf("GET /api/guide: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result []struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 airings, got %d", len(result))
	}
}

func TestGetGuide_NoDate(t *testing.T) {
	srv := newSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/guide")
	if err != nil {
		t.Fatalf("GET /api/guide: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestGetGuide_EmptyDate_ReturnsEmptyArray(t *testing.T) {
	srv := newSeededServer(t)
	// Use a date far in the future with no airings
	resp, err := http.Get(srv.URL + "/api/guide?date=2099-01-01")
	if err != nil {
		t.Fatalf("GET /api/guide: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Read the raw JSON body — must be "[]", not "null"
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed != "[]" {
		t.Fatalf("expected empty JSON array '[]', got %q", trimmed)
	}
}

func TestGetGuide_InvalidDate(t *testing.T) {
	srv := newSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/guide?date=notadate")
	if err != nil {
		t.Fatalf("GET /api/guide: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGetStatus_SourceURL(t *testing.T) {
	srv := newSeededServer(t)
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
	if result.SourceUrl != "http://test-source" {
		t.Errorf("sourceUrl: expected %q, got %q", "http://test-source", result.SourceUrl)
	}
}

func TestGetStatus_ContentType(t *testing.T) {
	srv := newSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/status")
	if err != nil {
		t.Fatalf("GET /api/status: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: expected application/json, got %q", ct)
	}
}

// TestGetChannelIcon_ServesFile verifies that GET /images/channel/{id} returns
// the cached icon with the correct Content-Type.
func TestGetChannelIcon_ServesFile(t *testing.T) {
	iconSrv := startFakeIconServer(t)
	srv, _ := newSeededServerWithIcons(t, iconSrv)

	resp, err := http.Get(srv.URL + "/images/channel/ch1")
	if err != nil {
		t.Fatalf("GET /images/channel/ch1: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "image/png") {
		t.Errorf("Content-Type: expected image/png, got %q", ct)
	}
}

// TestGetChannelIcon_ReturnsNotFoundForNoIcon verifies that GET /images/channel/{id}
// returns 404 when the channel has no icon.
func TestGetChannelIcon_ReturnsNotFoundForNoIcon(t *testing.T) {
	iconSrv := startFakeIconServer(t)
	srv, _ := newSeededServerWithIcons(t, iconSrv)

	resp, err := http.Get(srv.URL + "/images/channel/ch2")
	if err != nil {
		t.Fatalf("GET /images/channel/ch2: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for channel without icon, got %d", resp.StatusCode)
	}
}

// TestGetChannelIcon_ReturnsNotFoundForUnknownChannel verifies that
// GET /images/channel/{id} returns 404 for a channel ID that does not exist.
func TestGetChannelIcon_ReturnsNotFoundForUnknownChannel(t *testing.T) {
	srv := newSeededServer(t)

	resp, err := http.Get(srv.URL + "/images/channel/doesnotexist")
	if err != nil {
		t.Fatalf("GET /images/channel/doesnotexist: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown channel, got %d", resp.StatusCode)
	}
}

// TestGetChannelIcon_RedownloadsIfMissing verifies that GET /images/channel/{id}
// transparently re-downloads and serves the icon when the cached file is missing.
func TestGetChannelIcon_RedownloadsIfMissing(t *testing.T) {
	iconSrv := startFakeIconServer(t)
	srv, db := newSeededServerWithIcons(t, iconSrv)

	// Get the icon once to populate the cache.
	resp, err := http.Get(srv.URL + "/images/channel/ch1")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("initial icon request: status=%d err=%v", resp.StatusCode, err)
	}
	resp.Body.Close()

	// Delete the cached file to simulate a missing cache entry.
	localPath, err := db.EnsureChannelIcon(context.Background(), "ch1")
	if err != nil || localPath == "" {
		t.Fatalf("EnsureChannelIcon: path=%q err=%v", localPath, err)
	}
	os.Remove(localPath)

	// The handler should re-download and still return 200.
	resp2, err := http.Get(srv.URL + "/images/channel/ch1")
	if err != nil {
		t.Fatalf("GET after cache delete: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after re-download, got %d", resp2.StatusCode)
	}
}

// TestGetChannels_IconIsProxyURL verifies that /api/channels returns the proxy
// URL for channels that have an icon.
// newSearchSeededServer creates a test API server with search-relevant data:
// future and past airings across two channels, with varied categories, repeats,
// and text in titles/subtitles/descriptions.
func newSearchSeededServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test-source", images.NewCache(&http.Client{}, filepath.Join(dir, "images")), nil, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	now := time.Now()
	tv := &xmltv.TV{
		Channels: []xmltv.Channel{
			{ID: "ch1", DisplayNames: []xmltv.Name{{Value: "ABC"}}},
			{ID: "ch2", DisplayNames: []xmltv.Name{{Value: "SBS"}}},
		},
		Programmes: []xmltv.Programme{
			{
				// Future, non-repeat, title "Morning News", category News
				Start:      xmltv.XmltvTime{Time: now.Add(1 * time.Hour)},
				Stop:       xmltv.XmltvTime{Time: now.Add(2 * time.Hour)},
				Channel:    "ch1",
				Titles:     []xmltv.Name{{Value: "Morning News"}},
				Descs:      []xmltv.Name{{Value: "Start your day with headlines."}},
				Categories: []xmltv.Name{{Value: "News"}},
			},
			{
				// Future, repeat, title "Morning News" (same title, different channel)
				Start:           xmltv.XmltvTime{Time: now.Add(3 * time.Hour)},
				Stop:            xmltv.XmltvTime{Time: now.Add(4 * time.Hour)},
				Channel:         "ch2",
				Titles:          []xmltv.Name{{Value: "Morning News"}},
				Descs:           []xmltv.Name{{Value: "Replay of this morning's news."}},
				Categories:      []xmltv.Name{{Value: "News"}},
				PreviouslyShown: &xmltv.PreviouslyShown{},
			},
			{
				// Future, non-repeat, description mentions "news" but title doesn't
				Start:      xmltv.XmltvTime{Time: now.Add(5 * time.Hour)},
				Stop:       xmltv.XmltvTime{Time: now.Add(6 * time.Hour)},
				Channel:    "ch2",
				Titles:     []xmltv.Name{{Value: "Documentary Special"}},
				SubTitles:  []xmltv.Name{{Value: "Behind the news desk"}},
				Descs:      []xmltv.Name{{Value: "A look at how news is made."}},
				Categories: []xmltv.Name{{Value: "Documentary"}},
			},
			{
				// Past, non-repeat, title "Late Night News"
				Start:      xmltv.XmltvTime{Time: now.Add(-3 * time.Hour)},
				Stop:       xmltv.XmltvTime{Time: now.Add(-2 * time.Hour)},
				Channel:    "ch1",
				Titles:     []xmltv.Name{{Value: "Late Night News"}},
				Descs:      []xmltv.Name{{Value: "Yesterday's late night news."}},
				Categories: []xmltv.Name{{Value: "News"}},
			},
			{
				// Future, non-repeat, category Sport+Entertainment
				Start:      xmltv.XmltvTime{Time: now.Add(7 * time.Hour)},
				Stop:       xmltv.XmltvTime{Time: now.Add(8 * time.Hour)},
				Channel:    "ch2",
				Titles:     []xmltv.Name{{Value: "Sports Tonight"}},
				Descs:      []xmltv.Name{{Value: "All the sport highlights."}},
				Categories: []xmltv.Name{{Value: "Sport"}, {Value: "Entertainment"}},
			},
		},
	}

	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	mux := http.NewServeMux()
	handler := api.New(db, 0)
	handler.RegisterRoutes(mux)

	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})
	return srv
}

// --- Search endpoint tests ---

func TestSearch_MissingQuery_Returns400(t *testing.T) {
	srv := newSearchSeededServer(t)

	// No q parameter
	resp, err := http.Get(srv.URL + "/api/search")
	if err != nil {
		t.Fatalf("GET /api/search: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing q, got %d", resp.StatusCode)
	}

	// Empty q parameter
	resp2, err := http.Get(srv.URL + "/api/search?q=")
	if err != nil {
		t.Fatalf("GET /api/search?q=: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty q, got %d", resp2.StatusCode)
	}
}

func TestSearch_SimpleMode_ReturnsTitleMatches(t *testing.T) {
	srv := newSearchSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/search?q=News")
	if err != nil {
		t.Fatalf("GET /api/search: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
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

	// Simple mode: should match title only, future only
	titles := map[string]bool{}
	for _, g := range result {
		titles[g.Title] = true
	}
	if titles["Documentary Special"] {
		t.Error("simple search should not match description-only results")
	}
	if titles["Late Night News"] {
		t.Error("simple search should not return past airings")
	}
	if !titles["Morning News"] {
		t.Error("expected 'Morning News' in results")
	}
}

func TestSearch_SimpleMode_GroupsByTitle(t *testing.T) {
	srv := newSearchSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/search?q=News")
	if err != nil {
		t.Fatalf("GET /api/search: %v", err)
	}
	defer resp.Body.Close()

	var result []struct {
		Title   string `json:"title"`
		Airings []struct {
			ChannelID string    `json:"channelId"`
			StartTime time.Time `json:"startTime"`
		} `json:"airings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// "Morning News" appears on ch1 and ch2 — should be grouped
	for _, g := range result {
		if g.Title == "Morning News" {
			if len(g.Airings) < 2 {
				t.Errorf("expected at least 2 airings for 'Morning News' group, got %d", len(g.Airings))
			}
			// Verify sorted by start time ascending
			for i := 1; i < len(g.Airings); i++ {
				if g.Airings[i].StartTime.Before(g.Airings[i-1].StartTime) {
					t.Error("airings within group should be sorted by start time ascending")
				}
			}
			return
		}
	}
	t.Error("expected 'Morning News' group in results")
}

func TestSearch_SimpleMode_ChannelNamePopulated(t *testing.T) {
	srv := newSearchSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/search?q=News")
	if err != nil {
		t.Fatalf("GET /api/search: %v", err)
	}
	defer resp.Body.Close()

	var result []struct {
		Title   string `json:"title"`
		Airings []struct {
			ChannelName string `json:"channelName"`
		} `json:"airings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	for _, g := range result {
		for _, a := range g.Airings {
			if a.ChannelName == "" {
				t.Errorf("channelName should be populated for airing in group %q", g.Title)
			}
		}
	}
}

func TestSearch_SimpleMode_ExcludesRepeats(t *testing.T) {
	srv := newSearchSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/search?q=News&include_repeats=false")
	if err != nil {
		t.Fatalf("GET /api/search: %v", err)
	}
	defer resp.Body.Close()

	var result []struct {
		Title   string `json:"title"`
		Airings []struct {
			IsRepeat bool `json:"isRepeat"`
		} `json:"airings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, g := range result {
		for _, a := range g.Airings {
			if a.IsRepeat {
				t.Errorf("include_repeats=false should exclude repeats in group %q", g.Title)
			}
		}
	}
}

func TestSearch_AdvancedMode_MatchesDescription(t *testing.T) {
	srv := newSearchSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/search?q=news&mode=advanced")
	if err != nil {
		t.Fatalf("GET /api/search: %v", err)
	}
	defer resp.Body.Close()

	var result []struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	titles := map[string]bool{}
	for _, g := range result {
		titles[g.Title] = true
	}
	if !titles["Documentary Special"] {
		t.Error("advanced search should match 'Documentary Special' via subtitle/description")
	}
}

func TestSearch_AdvancedMode_CategoryFilter(t *testing.T) {
	srv := newSearchSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/search?q=news&mode=advanced&categories=Documentary")
	if err != nil {
		t.Fatalf("GET /api/search: %v", err)
	}
	defer resp.Body.Close()

	var result []struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	for _, g := range result {
		if g.Title == "Morning News" {
			t.Error("category filter should exclude 'Morning News' (not in Documentary category)")
		}
	}
	titles := map[string]bool{}
	for _, g := range result {
		titles[g.Title] = true
	}
	if !titles["Documentary Special"] {
		t.Error("expected 'Documentary Special' in filtered results")
	}
}

func TestSearch_AdvancedMode_IncludePast(t *testing.T) {
	srv := newSearchSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/search?q=News&mode=advanced&include_past=true")
	if err != nil {
		t.Fatalf("GET /api/search: %v", err)
	}
	defer resp.Body.Close()

	var result []struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	titles := map[string]bool{}
	for _, g := range result {
		titles[g.Title] = true
	}
	if !titles["Late Night News"] {
		t.Error("include_past=true should include past airings like 'Late Night News'")
	}
}

func TestSearch_ContentType(t *testing.T) {
	srv := newSearchSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/search?q=News")
	if err != nil {
		t.Fatalf("GET /api/search: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: expected application/json, got %q", ct)
	}
}

// --- Categories endpoint tests ---

func TestCategories_ReturnsSortedList(t *testing.T) {
	srv := newSearchSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/categories")
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

	expected := []string{"Documentary", "Entertainment", "News", "Sport"}
	if len(result) != len(expected) {
		t.Fatalf("expected %d categories, got %d: %v", len(expected), len(result), result)
	}
	for i, c := range result {
		if c != expected[i] {
			t.Errorf("category[%d]: expected %q, got %q", i, expected[i], c)
		}
	}
}

func TestCategories_EmptyWhenNoData(t *testing.T) {
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test-source", images.NewCache(&http.Client{}, filepath.Join(dir, "images")), nil, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	mux := http.NewServeMux()
	handler := api.New(db, 0)
	handler.RegisterRoutes(mux)

	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})

	resp, err := http.Get(srv.URL + "/api/categories")
	if err != nil {
		t.Fatalf("GET /api/categories: %v", err)
	}
	defer resp.Body.Close()

	var result []string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 categories, got %d", len(result))
	}
}

// TestSearch_AiringsOrderedByStartTime verifies that airings within a search
// result group are always ordered by start time ascending, even when FTS ranks
// would produce a different order.
func TestSearch_AiringsOrderedByStartTime(t *testing.T) {
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test-source", images.NewCache(&http.Client{}, filepath.Join(dir, "images")), nil, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	now := time.Now()
	tv := &xmltv.TV{
		Channels: []xmltv.Channel{
			{ID: "ch1", DisplayNames: []xmltv.Name{{Value: "ABC"}}},
			{ID: "ch2", DisplayNames: []xmltv.Name{{Value: "SBS"}}},
			{ID: "ch3", DisplayNames: []xmltv.Name{{Value: "TEN"}}},
		},
		Programmes: []xmltv.Programme{
			{
				// Earliest airing — title match only
				Start:   xmltv.XmltvTime{Time: now.Add(1 * time.Hour)},
				Stop:    xmltv.XmltvTime{Time: now.Add(2 * time.Hour)},
				Channel: "ch1",
				Titles:  []xmltv.Name{{Value: "Cricket Live"}},
				Descs:   []xmltv.Name{{Value: "Coverage of the match."}},
			},
			{
				// Latest airing — "cricket" in title + subtitle + description (better rank)
				Start:     xmltv.XmltvTime{Time: now.Add(5 * time.Hour)},
				Stop:      xmltv.XmltvTime{Time: now.Add(6 * time.Hour)},
				Channel:   "ch3",
				Titles:    []xmltv.Name{{Value: "Cricket Live"}},
				SubTitles: []xmltv.Name{{Value: "Cricket World Cup"}},
				Descs:     []xmltv.Name{{Value: "Live cricket from the World Cup."}},
			},
			{
				// Middle airing — title match only
				Start:   xmltv.XmltvTime{Time: now.Add(3 * time.Hour)},
				Stop:    xmltv.XmltvTime{Time: now.Add(4 * time.Hour)},
				Channel: "ch2",
				Titles:  []xmltv.Name{{Value: "Cricket Live"}},
				Descs:   []xmltv.Name{{Value: "Afternoon session."}},
			},
		},
	}

	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	mux := http.NewServeMux()
	handler := api.New(db, 0)
	handler.RegisterRoutes(mux)

	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})

	resp, err := http.Get(srv.URL + "/api/search?q=cricket&mode=advanced")
	if err != nil {
		t.Fatalf("GET /api/search: %v", err)
	}
	defer resp.Body.Close()

	var result []struct {
		Title   string `json:"title"`
		Airings []struct {
			ChannelID string    `json:"channelId"`
			StartTime time.Time `json:"startTime"`
		} `json:"airings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(result) == 0 {
		t.Fatal("expected at least one result group")
	}

	for _, g := range result {
		if g.Title == "Cricket Live" {
			if len(g.Airings) != 3 {
				t.Fatalf("expected 3 airings for 'Cricket Live', got %d", len(g.Airings))
			}
			for i := 1; i < len(g.Airings); i++ {
				if g.Airings[i].StartTime.Before(g.Airings[i-1].StartTime) {
					t.Errorf("airings not in chronological order: airing[%d] (%s) is before airing[%d] (%s)",
						i, g.Airings[i].StartTime.Format(time.RFC3339),
						i-1, g.Airings[i-1].StartTime.Format(time.RFC3339))
				}
			}
			return
		}
	}
	t.Error("expected 'Cricket Live' group in results")
}

// fixedClock implements database.Clock with a pinned time, making time-dependent
// search queries deterministic regardless of when the test runs.
type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func TestSearch_TodayFilter_ExcludesTomorrow(t *testing.T) {
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test-source", images.NewCache(&http.Client{}, filepath.Join(dir, "images")), nil, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Pin the clock to 09:00 today so search queries use a consistent "now".
	// Airings are placed at 10:00 (today) and 10:00 (tomorrow) so that:
	//   - stop_time > pinned_now  ✓  (11:00 > 09:00)
	//   - today filter correctly includes/excludes based on start_time vs midnight
	now := time.Now()
	pinnedNow := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, time.Local)
	db.SetClock(fixedClock{t: pinnedNow})

	todayAiring := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, time.Local)
	tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 10, 0, 0, 0, time.Local)
	tv := &xmltv.TV{
		Channels: []xmltv.Channel{
			{ID: "ch1", DisplayNames: []xmltv.Name{{Value: "ABC"}}},
		},
		Programmes: []xmltv.Programme{
			{
				Start:   xmltv.XmltvTime{Time: todayAiring},
				Stop:    xmltv.XmltvTime{Time: todayAiring.Add(1 * time.Hour)},
				Channel: "ch1",
				Titles:  []xmltv.Name{{Value: "Today Show"}},
			},
			{
				Start:   xmltv.XmltvTime{Time: tomorrow},
				Stop:    xmltv.XmltvTime{Time: tomorrow.Add(1 * time.Hour)},
				Channel: "ch1",
				Titles:  []xmltv.Name{{Value: "Tomorrow Show"}},
			},
		},
	}

	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	mux := http.NewServeMux()
	handler := api.New(db, 0)
	handler.RegisterRoutes(mux)

	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})

	// With today=true, only today's airing should be returned
	resp, err := http.Get(srv.URL + "/api/search?q=Show&today=true")
	if err != nil {
		t.Fatalf("GET /api/search: %v", err)
	}
	defer resp.Body.Close()

	var result []struct {
		Title   string `json:"title"`
		Airings []struct {
			StartTime time.Time `json:"startTime"`
		} `json:"airings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	titles := map[string]bool{}
	for _, g := range result {
		titles[g.Title] = true
	}
	if !titles["Today Show"] {
		t.Error("expected 'Today Show' in results with today=true")
	}
	if titles["Tomorrow Show"] {
		t.Error("'Tomorrow Show' should be excluded with today=true")
	}

	// Without today filter, both should be returned
	resp2, err := http.Get(srv.URL + "/api/search?q=Show")
	if err != nil {
		t.Fatalf("GET /api/search: %v", err)
	}
	defer resp2.Body.Close()

	var result2 []struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&result2); err != nil {
		t.Fatalf("decode: %v", err)
	}

	titles2 := map[string]bool{}
	for _, g := range result2 {
		titles2[g.Title] = true
	}
	if !titles2["Tomorrow Show"] {
		t.Error("expected 'Tomorrow Show' in results without today filter")
	}
}

func TestSearch_TodayFilter_AdvancedMode(t *testing.T) {
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test-source", images.NewCache(&http.Client{}, filepath.Join(dir, "images")), nil, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Pin the clock to 09:00 today — same rationale as TestSearch_TodayFilter_ExcludesTomorrow.
	now := time.Now()
	pinnedNow := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, time.Local)
	db.SetClock(fixedClock{t: pinnedNow})

	todayAiring := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, time.Local)
	tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 10, 0, 0, 0, time.Local)
	tv := &xmltv.TV{
		Channels: []xmltv.Channel{
			{ID: "ch1", DisplayNames: []xmltv.Name{{Value: "ABC"}}},
		},
		Programmes: []xmltv.Programme{
			{
				Start:      xmltv.XmltvTime{Time: todayAiring},
				Stop:       xmltv.XmltvTime{Time: todayAiring.Add(1 * time.Hour)},
				Channel:    "ch1",
				Titles:     []xmltv.Name{{Value: "Today Match"}},
				Categories: []xmltv.Name{{Value: "Sport"}},
			},
			{
				Start:      xmltv.XmltvTime{Time: tomorrow},
				Stop:       xmltv.XmltvTime{Time: tomorrow.Add(1 * time.Hour)},
				Channel:    "ch1",
				Titles:     []xmltv.Name{{Value: "Tomorrow Match"}},
				Categories: []xmltv.Name{{Value: "Sport"}},
			},
		},
	}

	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	mux := http.NewServeMux()
	handler := api.New(db, 0)
	handler.RegisterRoutes(mux)

	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})

	resp, err := http.Get(srv.URL + "/api/search?q=Match&mode=advanced&today=true")
	if err != nil {
		t.Fatalf("GET /api/search: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "Today Match") {
		t.Error("expected 'Today Match' in advanced search with today=true")
	}
	if strings.Contains(bodyStr, "Tomorrow Match") {
		t.Error("'Tomorrow Match' should be excluded in advanced search with today=true")
	}
}

func TestCategories_ContentType(t *testing.T) {
	srv := newSearchSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/categories")
	if err != nil {
		t.Fatalf("GET /api/categories: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: expected application/json, got %q", ct)
	}
}

func TestGetChannels_IconIsProxyURL(t *testing.T) {
	iconSrv := startFakeIconServer(t)
	srv, _ := newSeededServerWithIcons(t, iconSrv)

	resp, err := http.Get(srv.URL + "/api/channels")
	if err != nil {
		t.Fatalf("GET /api/channels: %v", err)
	}
	defer resp.Body.Close()

	var result []struct {
		ID   string `json:"id"`
		Icon string `json:"icon"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	var ch1, ch2 *struct {
		ID   string `json:"id"`
		Icon string `json:"icon"`
	}
	for i := range result {
		if result[i].ID == "ch1" {
			ch1 = &result[i]
		}
		if result[i].ID == "ch2" {
			ch2 = &result[i]
		}
	}
	if ch1 == nil {
		t.Fatal("ch1 not found in response")
	}
	if ch1.Icon != "/images/channel/ch1" {
		t.Errorf("ch1 icon: expected %q, got %q", "/images/channel/ch1", ch1.Icon)
	}
	if ch2 == nil {
		t.Fatal("ch2 not found in response")
	}
	if ch2.Icon != "" {
		t.Errorf("ch2 icon: expected empty (no icon), got %q", ch2.Icon)
	}
}

// --- Browse mode search tests (q optional) ---

// newBrowseSeededServer creates a test API server that includes premiere airings
// for testing the browse-mode search (q-less queries).
func newBrowseSeededServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test-source", images.NewCache(&http.Client{}, filepath.Join(dir, "images")), nil, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	now := time.Now()
	tv := &xmltv.TV{
		Channels: []xmltv.Channel{
			{ID: "ch1", DisplayNames: []xmltv.Name{{Value: "ABC"}}},
			{ID: "ch2", DisplayNames: []xmltv.Name{{Value: "SBS"}}},
		},
		Programmes: []xmltv.Programme{
			{
				// Future, premiere, category News
				Start:      xmltv.XmltvTime{Time: now.Add(1 * time.Hour)},
				Stop:       xmltv.XmltvTime{Time: now.Add(2 * time.Hour)},
				Channel:    "ch1",
				Titles:     []xmltv.Name{{Value: "New Drama"}},
				Categories: []xmltv.Name{{Value: "Drama"}},
				Premiere:   &xmltv.Name{Value: "First broadcast"},
			},
			{
				// Future, non-premiere, category Sport
				Start:      xmltv.XmltvTime{Time: now.Add(3 * time.Hour)},
				Stop:       xmltv.XmltvTime{Time: now.Add(4 * time.Hour)},
				Channel:    "ch2",
				Titles:     []xmltv.Name{{Value: "Sports Tonight"}},
				Categories: []xmltv.Name{{Value: "Sport"}},
			},
			{
				// Future, repeat, category Drama
				Start:           xmltv.XmltvTime{Time: now.Add(5 * time.Hour)},
				Stop:            xmltv.XmltvTime{Time: now.Add(6 * time.Hour)},
				Channel:         "ch2",
				Titles:          []xmltv.Name{{Value: "Old Drama Repeat"}},
				Categories:      []xmltv.Name{{Value: "Drama"}},
				PreviouslyShown: &xmltv.PreviouslyShown{},
			},
		},
	}

	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	mux := http.NewServeMux()
	handler := api.New(db, 0)
	handler.RegisterRoutes(mux)

	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})
	return srv
}

func TestSearch_EmptyQ_NoFilters_Returns400(t *testing.T) {
	srv := newBrowseSeededServer(t)

	// No q and no browse filters
	resp, err := http.Get(srv.URL + "/api/search")
	if err != nil {
		t.Fatalf("GET /api/search: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "at least one of q, is_premiere, or categories is required") {
		t.Errorf("expected specific error message, got: %q", string(body))
	}
}

func TestSearch_Browse_IsPremiere_Returns200(t *testing.T) {
	srv := newBrowseSeededServer(t)

	resp, err := http.Get(srv.URL + "/api/search?is_premiere=true")
	if err != nil {
		t.Fatalf("GET /api/search?is_premiere=true: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestSearch_Browse_IsPremiere_ReturnsOnlyPremieres(t *testing.T) {
	srv := newBrowseSeededServer(t)

	resp, err := http.Get(srv.URL + "/api/search?is_premiere=true")
	if err != nil {
		t.Fatalf("GET /api/search?is_premiere=true: %v", err)
	}
	defer resp.Body.Close()

	var result []struct {
		Title   string `json:"title"`
		Airings []struct {
			IsPremiere bool `json:"isPremiere"`
		} `json:"airings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(result) == 0 {
		t.Fatal("expected at least 1 result group")
	}
	for _, g := range result {
		for _, a := range g.Airings {
			if !a.IsPremiere {
				t.Errorf("browse is_premiere=true returned non-premiere in group %q", g.Title)
			}
		}
	}
	// "New Drama" should be present
	hasNewDrama := false
	for _, g := range result {
		if g.Title == "New Drama" {
			hasNewDrama = true
		}
	}
	if !hasNewDrama {
		t.Error("expected 'New Drama' in premiere results")
	}
	// "Sports Tonight" (non-premiere) should NOT be present
	for _, g := range result {
		if g.Title == "Sports Tonight" {
			t.Error("'Sports Tonight' should not appear in premiere results")
		}
	}
}

func TestSearch_Browse_Categories_Returns200(t *testing.T) {
	srv := newBrowseSeededServer(t)

	resp, err := http.Get(srv.URL + "/api/search?categories=Sport")
	if err != nil {
		t.Fatalf("GET /api/search?categories=Sport: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestSearch_Browse_Categories_ReturnsMatchingAirings(t *testing.T) {
	srv := newBrowseSeededServer(t)

	resp, err := http.Get(srv.URL + "/api/search?categories=Sport")
	if err != nil {
		t.Fatalf("GET /api/search?categories=Sport: %v", err)
	}
	defer resp.Body.Close()

	var result []struct {
		Title   string `json:"title"`
		Airings []struct {
			Categories []string `json:"categories"`
		} `json:"airings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(result) == 0 {
		t.Fatal("expected at least 1 result group")
	}
	for _, g := range result {
		for _, a := range g.Airings {
			hasSport := false
			for _, c := range a.Categories {
				if c == "Sport" {
					hasSport = true
					break
				}
			}
			if !hasSport {
				t.Errorf("browse categories=Sport returned airing without Sport in group %q", g.Title)
			}
		}
	}
	hasSportsTonightTitle := false
	for _, g := range result {
		if g.Title == "Sports Tonight" {
			hasSportsTonightTitle = true
		}
	}
	if !hasSportsTonightTitle {
		t.Error("expected 'Sports Tonight' in Sport category results")
	}
}

func TestSearch_Browse_NonEmptyQ_StillWorksAsSimpleSearch(t *testing.T) {
	srv := newBrowseSeededServer(t)

	resp, err := http.Get(srv.URL + "/api/search?q=Drama")
	if err != nil {
		t.Fatalf("GET /api/search?q=Drama: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result []struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	hasResult := false
	for _, g := range result {
		if strings.Contains(g.Title, "Drama") {
			hasResult = true
		}
	}
	if !hasResult {
		t.Error("q=Drama should still match via FTS on title")
	}
}

// --- RSS format tests ---

// rssRoot mirrors the RSS 2.0 XML structure for test parsing.
type rssRoot struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title         string    `xml:"title"`
	Description   string    `xml:"description"`
	LastBuildDate string    `xml:"lastBuildDate"`
	TTL           int       `xml:"ttl"`
	Items         []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string       `xml:"title"`
	Description string       `xml:"description"`
	PubDate     string       `xml:"pubDate"`
	GUID        rssGUID      `xml:"guid"`
	Categories  []string     `xml:"category"`
	Enclosure   rssEnclosure `xml:"enclosure"`
	Source      string       `xml:"source"`
}

type rssGUID struct {
	IsPermaLink string `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}

type rssEnclosure struct {
	URL  string `xml:"url,attr"`
	Type string `xml:"type,attr"`
}

// newRSSSeededServer creates a test API server with rich data for RSS tests.
// rssTTL is the server-wide default TTL (pass 0 for hard-coded default).
func newRSSSeededServer(t *testing.T, rssTTL int) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test-source", images.NewCache(&http.Client{}, filepath.Join(dir, "images")), nil, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	now := time.Now()
	tv := &xmltv.TV{
		Channels: []xmltv.Channel{
			{ID: "ch1", DisplayNames: []xmltv.Name{{Value: "ABC"}}},
			{ID: "ch2", DisplayNames: []xmltv.Name{{Value: "SBS"}}},
		},
		Programmes: []xmltv.Programme{
			{
				Start:           xmltv.XmltvTime{Time: now.Add(1 * time.Hour)},
				Stop:            xmltv.XmltvTime{Time: now.Add(2 * time.Hour)},
				Channel:         "ch1",
				Titles:      []xmltv.Name{{Value: "Morning News"}},
				SubTitles:   []xmltv.Name{{Value: "Early Edition"}},
				Descs:       []xmltv.Name{{Value: "Start your day with the latest headlines."}},
				Categories:  []xmltv.Name{{Value: "News"}, {Value: "Current Affairs"}},
				EpisodeNums: []xmltv.EpisodeNum{{System: "onscreen", Value: "S02E04"}},
				Icons:       []xmltv.Icon{{Src: "http://example.com/morning.jpg"}},
				StarRatings: []xmltv.StarRating{{Value: "8/10"}},
				Ratings:     []xmltv.Rating{{Value: "PG"}},
				Date:        "2026",
				Country:     []xmltv.Name{{Value: "Australia"}},
				Premiere:    &xmltv.Name{},
			},
			{
				Start:           xmltv.XmltvTime{Time: now.Add(3 * time.Hour)},
				Stop:            xmltv.XmltvTime{Time: now.Add(4 * time.Hour)},
				Channel:         "ch2",
				Titles:          []xmltv.Name{{Value: "Morning News"}},
				Descs:           []xmltv.Name{{Value: "Replay of this morning's news."}},
				Categories:      []xmltv.Name{{Value: "News"}},
				PreviouslyShown: &xmltv.PreviouslyShown{},
			},
			{
				Start:      xmltv.XmltvTime{Time: now.Add(5 * time.Hour)},
				Stop:       xmltv.XmltvTime{Time: now.Add(6 * time.Hour)},
				Channel:    "ch1",
				Titles:     []xmltv.Name{{Value: "Sports Tonight"}},
				Categories: []xmltv.Name{{Value: "Sport"}},
			},
		},
	}

	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	mux := http.NewServeMux()
	handler := api.New(db, rssTTL)
	handler.RegisterRoutes(mux)

	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})
	return srv
}

func parseRSS(t *testing.T, body []byte) rssRoot {
	t.Helper()
	var rss rssRoot
	if err := xml.Unmarshal(body, &rss); err != nil {
		t.Fatalf("failed to parse RSS XML: %v\nbody: %s", err, string(body))
	}
	return rss
}

func TestSearchRSS_ContentType(t *testing.T) {
	srv := newRSSSeededServer(t, 0)
	resp, err := http.Get(srv.URL + "/api/search?q=News&format=rss")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "application/rss+xml; charset=utf-8" {
		t.Errorf("Content-Type: expected %q, got %q", "application/rss+xml; charset=utf-8", ct)
	}
}

func TestSearchRSS_ValidXML(t *testing.T) {
	srv := newRSSSeededServer(t, 0)
	resp, err := http.Get(srv.URL + "/api/search?q=News&format=rss")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	rss := parseRSS(t, body)

	if rss.Version != "2.0" {
		t.Errorf("RSS version: expected 2.0, got %q", rss.Version)
	}
	if rss.Channel.Title != "TV Guide Search: News" {
		t.Errorf("channel title: expected %q, got %q", "TV Guide Search: News", rss.Channel.Title)
	}
}

func TestSearchRSS_ItemsNotGrouped(t *testing.T) {
	srv := newRSSSeededServer(t, 0)
	resp, err := http.Get(srv.URL + "/api/search?q=News&format=rss")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	rss := parseRSS(t, body)

	// "Morning News" has 2 airings — RSS should have them as separate items, not grouped
	if len(rss.Channel.Items) < 2 {
		t.Fatalf("expected at least 2 RSS items, got %d", len(rss.Channel.Items))
	}
}

func TestSearchRSS_SortedByStartTime(t *testing.T) {
	srv := newRSSSeededServer(t, 0)
	resp, err := http.Get(srv.URL + "/api/search?q=News&format=rss")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	rss := parseRSS(t, body)

	for i := 1; i < len(rss.Channel.Items); i++ {
		prev, _ := time.Parse(time.RFC1123Z, rss.Channel.Items[i-1].PubDate)
		curr, _ := time.Parse(time.RFC1123Z, rss.Channel.Items[i].PubDate)
		if curr.Before(prev) {
			t.Errorf("items not sorted by start time: item[%d] (%s) before item[%d] (%s)",
				i, rss.Channel.Items[i].PubDate, i-1, rss.Channel.Items[i-1].PubDate)
		}
	}
}

func TestSearchRSS_ItemFields(t *testing.T) {
	srv := newRSSSeededServer(t, 0)
	resp, err := http.Get(srv.URL + "/api/search?q=News&format=rss")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	rss := parseRSS(t, body)

	// Find the first "Morning News" item (ch1, has all fields)
	var item *rssItem
	for i := range rss.Channel.Items {
		if strings.Contains(rss.Channel.Items[i].Title, "Morning News") &&
			strings.Contains(rss.Channel.Items[i].Title, "Early Edition") {
			item = &rss.Channel.Items[i]
			break
		}
	}
	if item == nil {
		t.Fatal("could not find 'Morning News - Early Edition' item in RSS")
	}

	// Title should combine title + subtitle + episode
	if !strings.Contains(item.Title, "Morning News") {
		t.Error("item title missing 'Morning News'")
	}
	if !strings.Contains(item.Title, "Early Edition") {
		t.Error("item title missing subtitle 'Early Edition'")
	}
	if !strings.Contains(item.Title, "S02E04") {
		t.Error("item title missing episode number 'S02E04'")
	}

	// GUID format: channelId/startTimeRFC3339
	if item.GUID.IsPermaLink != "false" {
		t.Errorf("GUID isPermaLink: expected 'false', got %q", item.GUID.IsPermaLink)
	}
	if !strings.HasPrefix(item.GUID.Value, "ch1/") {
		t.Errorf("GUID should start with 'ch1/', got %q", item.GUID.Value)
	}

	// Categories
	if len(item.Categories) < 1 {
		t.Error("expected at least one category")
	}

	// Enclosure (icon)
	if item.Enclosure.URL != "http://example.com/morning.jpg" {
		t.Errorf("enclosure URL: expected %q, got %q", "http://example.com/morning.jpg", item.Enclosure.URL)
	}
	if item.Enclosure.Type != "image/jpeg" {
		t.Errorf("enclosure type: expected %q, got %q", "image/jpeg", item.Enclosure.Type)
	}

	// Source (channel name)
	if item.Source != "ABC" {
		t.Errorf("source: expected %q, got %q", "ABC", item.Source)
	}

	// PubDate should be RFC 2822
	if _, err := time.Parse(time.RFC1123Z, item.PubDate); err != nil {
		t.Errorf("pubDate not valid RFC 2822: %q, err: %v", item.PubDate, err)
	}
}

func TestSearchRSS_DescriptionHTML(t *testing.T) {
	srv := newRSSSeededServer(t, 0)
	resp, err := http.Get(srv.URL + "/api/search?q=News&format=rss")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	rss := parseRSS(t, body)

	// Find the rich item (ch1 Morning News)
	var item *rssItem
	for i := range rss.Channel.Items {
		if strings.Contains(rss.Channel.Items[i].Title, "Early Edition") {
			item = &rss.Channel.Items[i]
			break
		}
	}
	if item == nil {
		t.Fatal("could not find Morning News item")
	}

	desc := item.Description
	// Check various fields are present in the description HTML
	checks := []string{
		"<strong>Channel:</strong> ABC",
		"<strong>Time:</strong>",
		"Start your day with the latest headlines.",
		"<strong>Episode:</strong> S02E04",
		"<strong>Rating:</strong> 8/10",
		"<strong>Classification:</strong> PG",
		"<strong>Year:</strong> 2026",
		"<strong>Country:</strong> Australia",
		"<strong>Categories:</strong>",
		"<em>Premiere</em>",
	}
	for _, check := range checks {
		if !strings.Contains(desc, check) {
			t.Errorf("description missing %q\ndescription: %s", check, desc)
		}
	}
}

func TestSearchRSS_DescriptionOmitsEmptyFields(t *testing.T) {
	srv := newRSSSeededServer(t, 0)
	resp, err := http.Get(srv.URL + "/api/search?q=Sports&format=rss")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	rss := parseRSS(t, body)

	if len(rss.Channel.Items) == 0 {
		t.Fatal("expected at least one RSS item for Sports")
	}

	desc := rss.Channel.Items[0].Description
	// Sports Tonight has no episode, rating, classification, year, country
	shouldNotContain := []string{
		"<strong>Episode:</strong>",
		"<strong>Rating:</strong>",
		"<strong>Classification:</strong>",
		"<strong>Year:</strong>",
		"<strong>Country:</strong>",
		"<em>Premiere</em>",
	}
	for _, check := range shouldNotContain {
		if strings.Contains(desc, check) {
			t.Errorf("description should not contain %q for sparse airing\ndescription: %s", check, desc)
		}
	}
}

func TestSearchRSS_RepeatFlag(t *testing.T) {
	srv := newRSSSeededServer(t, 0)
	resp, err := http.Get(srv.URL + "/api/search?q=News&format=rss")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	rss := parseRSS(t, body)

	// The ch2 Morning News is a repeat
	for _, item := range rss.Channel.Items {
		if item.Source == "SBS" && strings.Contains(item.Title, "Morning News") {
			if !strings.Contains(item.Description, "<em>Repeat</em>") {
				t.Error("ch2 Morning News should have Repeat flag in description")
			}
			return
		}
	}
	t.Error("could not find SBS Morning News item")
}

func TestSearchRSS_NoEnclosureWithoutIcon(t *testing.T) {
	srv := newRSSSeededServer(t, 0)
	resp, err := http.Get(srv.URL + "/api/search?q=Sports&format=rss")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	rss := parseRSS(t, body)

	if len(rss.Channel.Items) == 0 {
		t.Fatal("expected at least one RSS item")
	}
	// Sports Tonight has no icon — enclosure should be empty
	if rss.Channel.Items[0].Enclosure.URL != "" {
		t.Errorf("expected no enclosure for airing without icon, got URL=%q", rss.Channel.Items[0].Enclosure.URL)
	}
}

// --- TTL tests ---

func TestSearchRSS_DefaultTTL(t *testing.T) {
	srv := newRSSSeededServer(t, 0)
	resp, err := http.Get(srv.URL + "/api/search?q=News&format=rss")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	rss := parseRSS(t, body)

	if rss.Channel.TTL != 360 {
		t.Errorf("default TTL: expected 360, got %d", rss.Channel.TTL)
	}
}

func TestSearchRSS_EnvVarTTL(t *testing.T) {
	srv := newRSSSeededServer(t, 120)
	resp, err := http.Get(srv.URL + "/api/search?q=News&format=rss")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	rss := parseRSS(t, body)

	if rss.Channel.TTL != 120 {
		t.Errorf("env var TTL: expected 120, got %d", rss.Channel.TTL)
	}
}

func TestSearchRSS_QueryParamTTL(t *testing.T) {
	srv := newRSSSeededServer(t, 120)
	resp, err := http.Get(srv.URL + "/api/search?q=News&format=rss&ttl=30")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	rss := parseRSS(t, body)

	if rss.Channel.TTL != 30 {
		t.Errorf("query param TTL: expected 30, got %d", rss.Channel.TTL)
	}
}

func TestSearchRSS_InvalidQueryParamTTL_FallsBackToEnvVar(t *testing.T) {
	srv := newRSSSeededServer(t, 120)
	resp, err := http.Get(srv.URL + "/api/search?q=News&format=rss&ttl=notanumber")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	rss := parseRSS(t, body)

	if rss.Channel.TTL != 120 {
		t.Errorf("invalid query TTL should fall back to env var TTL (120), got %d", rss.Channel.TTL)
	}
}

func TestSearchRSS_ZeroQueryParamTTL_FallsBack(t *testing.T) {
	srv := newRSSSeededServer(t, 120)
	resp, err := http.Get(srv.URL + "/api/search?q=News&format=rss&ttl=0")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	rss := parseRSS(t, body)

	if rss.Channel.TTL != 120 {
		t.Errorf("zero query TTL should fall back to env var TTL (120), got %d", rss.Channel.TTL)
	}
}

func TestSearchRSS_NegativeQueryParamTTL_FallsBack(t *testing.T) {
	srv := newRSSSeededServer(t, 120)
	resp, err := http.Get(srv.URL + "/api/search?q=News&format=rss&ttl=-5")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	rss := parseRSS(t, body)

	if rss.Channel.TTL != 120 {
		t.Errorf("negative query TTL should fall back to env var TTL (120), got %d", rss.Channel.TTL)
	}
}

// --- Verify existing JSON behaviour unchanged ---

func TestSearchRSS_JSONUnchangedWithoutFormat(t *testing.T) {
	srv := newRSSSeededServer(t, 0)
	resp, err := http.Get(srv.URL + "/api/search?q=News")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected JSON Content-Type without format param, got %q", ct)
	}

	var result []struct {
		Title   string `json:"title"`
		Airings []struct {
			ChannelID string `json:"channelId"`
		} `json:"airings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("should still return valid JSON: %v", err)
	}
	// Should be grouped by title
	for _, g := range result {
		if g.Title == "Morning News" && len(g.Airings) < 2 {
			t.Error("JSON response should still group airings by title")
		}
	}
}

func TestSearchRSS_WorksWithAllSearchParams(t *testing.T) {
	srv := newRSSSeededServer(t, 0)
	// Test format=rss with advanced mode and categories
	resp, err := http.Get(srv.URL + "/api/search?q=News&format=rss&mode=advanced&categories=News")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	rss := parseRSS(t, body)

	// Should have items (Morning News matches)
	if len(rss.Channel.Items) == 0 {
		t.Error("expected RSS items for advanced search with categories")
	}

	// All items should have News category
	for _, item := range rss.Channel.Items {
		hasNews := false
		for _, cat := range item.Categories {
			if cat == "News" {
				hasNews = true
				break
			}
		}
		if !hasNews {
			t.Errorf("expected all items to have News category, item %q has %v", item.Title, item.Categories)
		}
	}
}

func TestSearchRSS_ChannelDescription(t *testing.T) {
	srv := newRSSSeededServer(t, 0)

	// Simple mode
	resp, err := http.Get(srv.URL + "/api/search?q=News&format=rss")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	rss := parseRSS(t, body)

	if rss.Channel.Description == "" {
		t.Error("channel description should not be empty")
	}

	// Advanced mode with categories
	resp2, err := http.Get(srv.URL + "/api/search?q=News&format=rss&mode=advanced&categories=News,Sport")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp2.Body.Close()

	body2, _ := io.ReadAll(resp2.Body)
	rss2 := parseRSS(t, body2)

	if !strings.Contains(rss2.Channel.Description, "advanced") {
		t.Error("advanced mode description should mention 'advanced'")
	}
}

func TestSearchRSS_LastBuildDate(t *testing.T) {
	srv := newRSSSeededServer(t, 0)
	before := time.Now()
	resp, err := http.Get(srv.URL + "/api/search?q=News&format=rss")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	rss := parseRSS(t, body)

	buildDate, err := time.Parse(time.RFC1123Z, rss.Channel.LastBuildDate)
	if err != nil {
		t.Fatalf("lastBuildDate not valid RFC 2822: %q, err: %v", rss.Channel.LastBuildDate, err)
	}
	if buildDate.Before(before.Add(-1*time.Second)) || buildDate.After(time.Now().Add(1*time.Second)) {
		t.Errorf("lastBuildDate should be approximately now, got %v", buildDate)
	}
}

// newNowNextServer creates a test server seeded with three channels and
// airings anchored to FIXED_NOW (2025-06-10T14:00:00Z):
//   - ch1 (lcn=1): Show A airing now (13:30–14:30), Show B coming next (14:30–15:00)
//   - ch2 (lcn=2): no current airing, Show C next (15:00–16:00)
//   - ch3 (no lcn): no airings at all
func newNowNextServer(t *testing.T) (*httptest.Server, time.Time) {
	t.Helper()
	fixedNow := time.Date(2025, 6, 10, 14, 0, 0, 0, time.UTC)

	dir := t.TempDir()
	db, err := database.Open(
		filepath.Join(dir, "test.db"), 7, "http://test-source",
		images.NewCache(&http.Client{}, filepath.Join(dir, "images")),
		nil, nil,
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	db.SetClock(fixedClock{t: fixedNow})

	tv := &xmltv.TV{
		Channels: []xmltv.Channel{
			{ID: "ch1", DisplayNames: []xmltv.Name{{Value: "ABC"}}, LCN: "1"},
			{ID: "ch2", DisplayNames: []xmltv.Name{{Value: "SBS"}}, LCN: "2"},
			{ID: "ch3", DisplayNames: []xmltv.Name{{Value: "Nine"}}},
		},
		Programmes: []xmltv.Programme{
			{
				Start:   xmltv.XmltvTime{Time: fixedNow.Add(-30 * time.Minute)},
				Stop:    xmltv.XmltvTime{Time: fixedNow.Add(30 * time.Minute)},
				Channel: "ch1",
				Titles:  []xmltv.Name{{Value: "Show A"}},
			},
			{
				Start:   xmltv.XmltvTime{Time: fixedNow.Add(30 * time.Minute)},
				Stop:    xmltv.XmltvTime{Time: fixedNow.Add(60 * time.Minute)},
				Channel: "ch1",
				Titles:  []xmltv.Name{{Value: "Show B"}},
			},
			{
				Start:   xmltv.XmltvTime{Time: fixedNow.Add(60 * time.Minute)},
				Stop:    xmltv.XmltvTime{Time: fixedNow.Add(120 * time.Minute)},
				Channel: "ch2",
				Titles:  []xmltv.Name{{Value: "Show C"}},
			},
		},
	}
	if err := db.Refresh(context.Background(), tv, fixedNow.Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	mux := http.NewServeMux()
	handler := api.New(db, 0)
	handler.RegisterRoutes(mux)

	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})
	return srv, fixedNow
}

func TestGetNowNext_API_StatusOK(t *testing.T) {
	srv, _ := newNowNextServer(t)
	resp, err := http.Get(srv.URL + "/api/explore/now-next")
	if err != nil {
		t.Fatalf("GET /api/explore/now-next: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestGetNowNext_API_ResponseShape(t *testing.T) {
	srv, fixedNow := newNowNextServer(t)
	resp, err := http.Get(srv.URL + "/api/explore/now-next")
	if err != nil {
		t.Fatalf("GET /api/explore/now-next: %v", err)
	}
	defer resp.Body.Close()

	var entries []struct {
		ChannelID   string `json:"channelId"`
		ChannelName string `json:"channelName"`
		Current     *struct {
			Title string    `json:"title"`
			Start time.Time `json:"start"`
			Stop  time.Time `json:"stop"`
		} `json:"current"`
		Next *struct {
			Title string    `json:"title"`
			Start time.Time `json:"start"`
		} `json:"next"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// ch1: current = Show A, next = Show B
	e0 := entries[0]
	if e0.ChannelID != "ch1" {
		t.Errorf("entries[0].channelId = %q, want ch1", e0.ChannelID)
	}
	if e0.ChannelName != "ABC" {
		t.Errorf("entries[0].channelName = %q, want ABC", e0.ChannelName)
	}
	if e0.Current == nil {
		t.Fatal("entries[0].current is nil, want Show A")
	}
	if e0.Current.Title != "Show A" {
		t.Errorf("entries[0].current.title = %q, want Show A", e0.Current.Title)
	}
	if e0.Next == nil {
		t.Fatal("entries[0].next is nil, want Show B")
	}
	if e0.Next.Title != "Show B" {
		t.Errorf("entries[0].next.title = %q, want Show B", e0.Next.Title)
	}

	// ch2: no current, next = Show C
	e1 := entries[1]
	if e1.ChannelID != "ch2" {
		t.Errorf("entries[1].channelId = %q, want ch2", e1.ChannelID)
	}
	if e1.Current != nil {
		t.Errorf("entries[1].current = %v, want null", e1.Current)
	}
	if e1.Next == nil {
		t.Fatal("entries[1].next is nil, want Show C")
	}
	if e1.Next.Title != "Show C" {
		t.Errorf("entries[1].next.title = %q, want Show C", e1.Next.Title)
	}

	// ch3: both null
	e2 := entries[2]
	if e2.ChannelID != "ch3" {
		t.Errorf("entries[2].channelId = %q, want ch3", e2.ChannelID)
	}
	if e2.Current != nil {
		t.Errorf("entries[2].current = %v, want null", e2.Current)
	}
	if e2.Next != nil {
		t.Errorf("entries[2].next = %v, want null", e2.Next)
	}

	_ = fixedNow // used for seeding; referenced to suppress lint
}
