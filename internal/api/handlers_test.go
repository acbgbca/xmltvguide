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
