package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test-source", filepath.Join(dir, "images"), &http.Client{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

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
				Start:   xmltv.XmltvTime{Time: time.Date(2026, 3, 29, 6, 0, 0, 0, time.UTC)},
				Stop:    xmltv.XmltvTime{Time: time.Date(2026, 3, 29, 7, 0, 0, 0, time.UTC)},
				Channel: "ch1",
				Titles:  []xmltv.Name{{Value: "Morning News"}},
			},
			{
				Start:      xmltv.XmltvTime{Time: time.Date(2026, 3, 29, 7, 0, 0, 0, time.UTC)},
				Stop:       xmltv.XmltvTime{Time: time.Date(2026, 3, 29, 8, 0, 0, 0, time.UTC)},
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
	handler := api.New(db)
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
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test-source", filepath.Join(dir, "images"), iconSrv.Client())
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
	handler := api.New(db)
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
	resp, err := http.Get(srv.URL + "/api/guide?date=2026-03-29")
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
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test-source", filepath.Join(dir, "images"), &http.Client{})
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
	handler := api.New(db)
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
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test-source", filepath.Join(dir, "images"), &http.Client{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	mux := http.NewServeMux()
	handler := api.New(db)
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
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test-source", filepath.Join(dir, "images"), &http.Client{})
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
	handler := api.New(db)
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
