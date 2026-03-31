package database_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/acbgbca/xmltvguide/internal/database"
	"github.com/acbgbca/xmltvguide/internal/xmltv"
)

func TestMain(m *testing.M) {
	time.Local = time.UTC
	os.Exit(m.Run())
}

// failingTransport rejects every request immediately — used in tests that do not
// exercise icon downloading so that no real network calls are made.
type failingTransport struct{}

func (f *failingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("no network in tests: %s", r.URL)
}

// openTestDB opens a fresh database with a failing HTTP client so that icon
// download attempts fail immediately without network I/O.
func openTestDB(t *testing.T) *database.DB {
	t.Helper()
	dir := t.TempDir()
	client := &http.Client{Transport: &failingTransport{}}
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test", filepath.Join(dir, "images"), client)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// openTestDBWithIconServer opens a fresh database configured to download icons
// from the given test server.
func openTestDBWithIconServer(t *testing.T, iconServer *httptest.Server) *database.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test", filepath.Join(dir, "images"), iconServer.Client())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// startIconServer starts a test HTTP server that serves a small PNG-like
// response (just enough bytes for Content-Type detection) for any request.
func startIconServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		// Minimal valid PNG header bytes so the content type is unambiguous.
		w.Write([]byte("\x89PNG\r\n\x1a\n"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// startStrictIconServer starts a test HTTP server that returns 406 unless the
// request includes both an Accept header and a User-Agent header, mimicking
// servers (e.g. xmltv.net) that enforce content negotiation.
func startStrictIconServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") == "" || r.Header.Get("User-Agent") == "" {
			w.WriteHeader(http.StatusNotAcceptable)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("\x89PNG\r\n\x1a\n"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestRefresh_IconDownload_SetsRequiredHeaders verifies that icon download
// requests include Accept and User-Agent headers. Servers such as xmltv.net
// return 406 when these headers are absent, causing icons to silently fail.
// The test uses a strict server that returns 406 unless both headers are
// present, then asserts that EnsureChannelIcon returns a valid local path
// (meaning the download actually succeeded — not just that icon_url was stored).
func TestRefresh_IconDownload_SetsRequiredHeaders(t *testing.T) {
	iconSrv := startStrictIconServer(t)
	tv := &xmltv.TV{
		Channels: []xmltv.Channel{
			{
				ID:           "ch1",
				DisplayNames: []xmltv.Name{{Value: "ABC"}},
				Icons:        []xmltv.Icon{{Src: iconSrv.URL + "/abc.png"}},
			},
		},
	}
	db := openTestDBWithIconServer(t, iconSrv)
	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// EnsureChannelIcon must return a valid local path, proving the download
	// succeeded. Without Accept/User-Agent headers the strict server returns 406,
	// Refresh stores icon="" (no local file), and EnsureChannelIcon subsequently
	// fails when it tries to re-download on demand.
	localPath, err := db.EnsureChannelIcon(context.Background(), "ch1")
	if err != nil {
		t.Fatalf("EnsureChannelIcon: %v — icon download likely failed due to missing Accept/User-Agent headers", err)
	}
	if localPath == "" {
		t.Error("expected non-empty local path; icon download likely failed due to missing request headers (Accept/User-Agent)")
	}
}

func sampleTV() *xmltv.TV {
	return &xmltv.TV{
		Channels: []xmltv.Channel{
			{
				ID:           "ch1",
				DisplayNames: []xmltv.Name{{Value: "ABC"}},
				Icons:        []xmltv.Icon{{Src: "https://example.com/abc.png"}},
				LCN:          "2",
			},
			{
				ID:           "ch2",
				DisplayNames: []xmltv.Name{{Value: "SBS"}},
			},
		},
		Programmes: []xmltv.Programme{
			{
				Start:       xmltv.XmltvTime{Time: time.Date(2026, 3, 28, 23, 0, 0, 0, time.UTC)},
				Stop:        xmltv.XmltvTime{Time: time.Date(2026, 3, 29, 1, 0, 0, 0, time.UTC)},
				Channel:     "ch2",
				Titles:      []xmltv.Name{{Value: "Late Night Movie"}},
				Descs:       []xmltv.Name{{Value: "A classic late night movie spanning midnight."}},
				Categories:  []xmltv.Name{{Value: "Movie"}},
			},
			{
				Start:      xmltv.XmltvTime{Time: time.Date(2026, 3, 29, 6, 0, 0, 0, time.UTC)},
				Stop:       xmltv.XmltvTime{Time: time.Date(2026, 3, 29, 7, 0, 0, 0, time.UTC)},
				Channel:    "ch1",
				Titles:     []xmltv.Name{{Value: "Morning News"}},
				Descs:      []xmltv.Name{{Value: "The latest news to start your day."}},
				Categories: []xmltv.Name{{Value: "News"}},
				Ratings:    []xmltv.Rating{{Value: "G", System: "ABA"}},
			},
			{
				Start:       xmltv.XmltvTime{Time: time.Date(2026, 3, 29, 7, 0, 0, 0, time.UTC)},
				Stop:        xmltv.XmltvTime{Time: time.Date(2026, 3, 29, 9, 0, 0, 0, time.UTC)},
				Channel:     "ch1",
				Titles:      []xmltv.Name{{Value: "Sunrise"}},
				SubTitles:   []xmltv.Name{{Value: "Monday Edition"}},
				Descs:       []xmltv.Name{{Value: "Morning breakfast television programme."}},
				Categories:  []xmltv.Name{{Value: "Entertainment"}},
				EpisodeNums: []xmltv.EpisodeNum{
					{Value: "5.12.0/1", System: "xmltv_ns"},
					{Value: "S06 E13", System: "onscreen"},
				},
				StarRatings:     []xmltv.StarRating{{Value: "3.5/5", System: "imdb"}},
				PreviouslyShown: &xmltv.PreviouslyShown{},
			},
			{
				Start:      xmltv.XmltvTime{Time: time.Date(2026, 3, 29, 6, 30, 0, 0, time.UTC)},
				Stop:       xmltv.XmltvTime{Time: time.Date(2026, 3, 29, 7, 30, 0, 0, time.UTC)},
				Channel:    "ch2",
				Titles:     []xmltv.Name{{Value: "World News"}},
				SubTitles:  []xmltv.Name{{Value: "International Edition"}},
				Descs:      []xmltv.Name{{Value: "Comprehensive international news coverage."}},
				Categories: []xmltv.Name{{Value: "News"}, {Value: "International"}},
				Date:       "2026",
				Premiere:   &xmltv.Name{Value: "First broadcast"},
			},
		},
	}
}

func TestOpen_EmptyDB(t *testing.T) {
	db := openTestDB(t)
	channels, err := db.GetChannels()
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}
	if channels == nil {
		t.Fatal("expected non-nil slice, got nil")
	}
	if len(channels) != 0 {
		t.Fatalf("expected 0 channels, got %d", len(channels))
	}
}

func TestRefresh_ChannelCount(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), sampleTV(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	channels, err := db.GetChannels()
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}
	if len(channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(channels))
	}
}

func TestRefresh_ChannelOrder(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), sampleTV(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	channels, err := db.GetChannels()
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}
	if len(channels) < 2 {
		t.Fatalf("expected at least 2 channels, got %d", len(channels))
	}
	if channels[0].ID != "ch1" {
		t.Errorf("expected first channel to be ch1, got %q", channels[0].ID)
	}
	if channels[1].ID != "ch2" {
		t.Errorf("expected second channel to be ch2, got %q", channels[1].ID)
	}
}

func TestRefresh_ChannelFields(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), sampleTV(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	channels, err := db.GetChannels()
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}
	if len(channels) < 2 {
		t.Fatalf("expected at least 2 channels, got %d", len(channels))
	}
	ch1 := channels[0]
	if ch1.DisplayName != "ABC" {
		t.Errorf("ch1 DisplayName: expected %q, got %q", "ABC", ch1.DisplayName)
	}
	// After migration, GetChannels returns the proxy URL (not the external URL).
	if ch1.Icon != "/images/channel/ch1" {
		t.Errorf("ch1 Icon: expected %q, got %q", "/images/channel/ch1", ch1.Icon)
	}
	ch2 := channels[1]
	if ch2.Icon != "" {
		t.Errorf("ch2 Icon: expected empty, got %q", ch2.Icon)
	}
}

func TestRefresh_ChannelLCN(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), sampleTV(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	channels, err := db.GetChannels()
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}
	if len(channels) < 2 {
		t.Fatalf("expected at least 2 channels, got %d", len(channels))
	}
	ch1 := channels[0]
	if ch1.LCN == nil || *ch1.LCN != 2 {
		t.Errorf("ch1 LCN: expected 2, got %v", ch1.LCN)
	}
	ch2 := channels[1]
	if ch2.LCN != nil {
		t.Errorf("ch2 LCN: expected nil, got %v", ch2.LCN)
	}
}

// TestRefresh_IconDownloaded verifies that when a channel has an icon URL and a
// working HTTP client, Refresh downloads the icon and the local file exists.
func TestRefresh_IconDownloaded(t *testing.T) {
	iconSrv := startIconServer(t)
	tv := &xmltv.TV{
		Channels: []xmltv.Channel{
			{
				ID:           "ch1",
				DisplayNames: []xmltv.Name{{Value: "ABC"}},
				Icons:        []xmltv.Icon{{Src: iconSrv.URL + "/abc.png"}},
			},
		},
	}
	db := openTestDBWithIconServer(t, iconSrv)
	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	channels, err := db.GetChannels()
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}
	if len(channels) == 0 {
		t.Fatal("expected channels")
	}
	if channels[0].Icon != "/images/channel/ch1" {
		t.Errorf("Icon: expected proxy URL, got %q", channels[0].Icon)
	}
}

// TestRefresh_IconSkipsIfUnchanged verifies that a second Refresh with the same
// icon URL and an existing local file does NOT make a second download request.
func TestRefresh_IconSkipsIfUnchanged(t *testing.T) {
	requestCount := 0
	iconSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("\x89PNG\r\n\x1a\n"))
	}))
	t.Cleanup(iconSrv.Close)

	tv := &xmltv.TV{
		Channels: []xmltv.Channel{
			{
				ID:           "ch1",
				DisplayNames: []xmltv.Name{{Value: "ABC"}},
				Icons:        []xmltv.Icon{{Src: iconSrv.URL + "/abc.png"}},
			},
		},
	}
	db := openTestDBWithIconServer(t, iconSrv)

	// First refresh: should download.
	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("expected 1 icon request after first refresh, got %d", requestCount)
	}

	// Second refresh with same URL: file already exists → should NOT download again.
	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("second Refresh: %v", err)
	}
	if requestCount != 1 {
		t.Errorf("expected still 1 icon request after second refresh (cached), got %d", requestCount)
	}
}

// TestRefresh_IconRedownloadsIfURLChanged verifies that when the icon URL changes
// across refreshes, the new image is downloaded.
func TestRefresh_IconRedownloadsIfURLChanged(t *testing.T) {
	requestCount := 0
	iconSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("\x89PNG\r\n\x1a\n"))
	}))
	t.Cleanup(iconSrv.Close)

	db := openTestDBWithIconServer(t, iconSrv)

	tv1 := &xmltv.TV{
		Channels: []xmltv.Channel{
			{
				ID:           "ch1",
				DisplayNames: []xmltv.Name{{Value: "ABC"}},
				Icons:        []xmltv.Icon{{Src: iconSrv.URL + "/abc-v1.png"}},
			},
		},
	}
	if err := db.Refresh(context.Background(), tv1, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("expected 1 request, got %d", requestCount)
	}

	// Change the icon URL.
	tv2 := &xmltv.TV{
		Channels: []xmltv.Channel{
			{
				ID:           "ch1",
				DisplayNames: []xmltv.Name{{Value: "ABC"}},
				Icons:        []xmltv.Icon{{Src: iconSrv.URL + "/abc-v2.png"}},
			},
		},
	}
	if err := db.Refresh(context.Background(), tv2, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("second Refresh: %v", err)
	}
	if requestCount != 2 {
		t.Errorf("expected 2 requests after URL change, got %d", requestCount)
	}
}

// TestEnsureChannelIcon_ReturnsPath verifies that EnsureChannelIcon returns the
// local file path when the icon has already been downloaded.
func TestEnsureChannelIcon_ReturnsPath(t *testing.T) {
	iconSrv := startIconServer(t)
	tv := &xmltv.TV{
		Channels: []xmltv.Channel{
			{
				ID:           "ch1",
				DisplayNames: []xmltv.Name{{Value: "ABC"}},
				Icons:        []xmltv.Icon{{Src: iconSrv.URL + "/abc.png"}},
			},
		},
	}
	db := openTestDBWithIconServer(t, iconSrv)
	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	localPath, err := db.EnsureChannelIcon(context.Background(), "ch1")
	if err != nil {
		t.Fatalf("EnsureChannelIcon: %v", err)
	}
	if localPath == "" {
		t.Fatal("expected non-empty local path")
	}
	if _, err := os.Stat(localPath); err != nil {
		t.Errorf("expected file to exist at %q: %v", localPath, err)
	}
}

// TestEnsureChannelIcon_RedownloadsIfMissing verifies that EnsureChannelIcon
// re-downloads the icon and returns a valid path when the cached file is missing.
func TestEnsureChannelIcon_RedownloadsIfMissing(t *testing.T) {
	iconSrv := startIconServer(t)
	tv := &xmltv.TV{
		Channels: []xmltv.Channel{
			{
				ID:           "ch1",
				DisplayNames: []xmltv.Name{{Value: "ABC"}},
				Icons:        []xmltv.Icon{{Src: iconSrv.URL + "/abc.png"}},
			},
		},
	}
	db := openTestDBWithIconServer(t, iconSrv)
	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// Get the path and delete the file to simulate a missing cache entry.
	localPath, err := db.EnsureChannelIcon(context.Background(), "ch1")
	if err != nil || localPath == "" {
		t.Fatalf("initial EnsureChannelIcon: path=%q err=%v", localPath, err)
	}
	os.Remove(localPath)

	// EnsureChannelIcon should re-download and return a valid path.
	newPath, err := db.EnsureChannelIcon(context.Background(), "ch1")
	if err != nil {
		t.Fatalf("EnsureChannelIcon after delete: %v", err)
	}
	if newPath == "" {
		t.Fatal("expected non-empty path after re-download")
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("expected re-downloaded file to exist at %q: %v", newPath, err)
	}
}

// TestEnsureChannelIcon_ReturnsEmptyIfNoIcon verifies that EnsureChannelIcon
// returns ("", nil) for a channel that has no icon.
func TestEnsureChannelIcon_ReturnsEmptyIfNoIcon(t *testing.T) {
	db := openTestDB(t)
	tv := &xmltv.TV{
		Channels: []xmltv.Channel{
			{ID: "ch1", DisplayNames: []xmltv.Name{{Value: "ABC"}}},
		},
	}
	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	localPath, err := db.EnsureChannelIcon(context.Background(), "ch1")
	if err != nil {
		t.Fatalf("EnsureChannelIcon: %v", err)
	}
	if localPath != "" {
		t.Errorf("expected empty path for channel without icon, got %q", localPath)
	}
}

// TestEnsureChannelIcon_ReturnsEmptyIfUnknownChannel verifies that
// EnsureChannelIcon returns ("", nil) for a channel ID that does not exist.
func TestEnsureChannelIcon_ReturnsEmptyIfUnknownChannel(t *testing.T) {
	db := openTestDB(t)
	localPath, err := db.EnsureChannelIcon(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("EnsureChannelIcon: %v", err)
	}
	if localPath != "" {
		t.Errorf("expected empty path for unknown channel, got %q", localPath)
	}
}

func TestGetAirings_SxxExxEpisodeMapping(t *testing.T) {
	db := openTestDB(t)
	tv := sampleTV()
	// Add a programme with SxxExx episode numbering (as used by this XMLTV source)
	tv.Programmes = append(tv.Programmes, xmltv.Programme{
		Start:   xmltv.XmltvTime{Time: time.Date(2026, 3, 29, 9, 0, 0, 0, time.UTC)},
		Stop:    xmltv.XmltvTime{Time: time.Date(2026, 3, 29, 10, 0, 0, 0, time.UTC)},
		Channel: "ch1",
		Titles:  []xmltv.Name{{Value: "Drama Show"}},
		EpisodeNums: []xmltv.EpisodeNum{
			{Value: "S02E04", System: "SxxExx"},
			{Value: "1.3.", System: "xmltv_ns"},
		},
	})
	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	date := time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC)
	airings, err := db.GetAirings(date)
	if err != nil {
		t.Fatalf("GetAirings: %v", err)
	}
	var drama *database.Airing
	for i := range airings {
		if airings[i].Title == "Drama Show" {
			drama = &airings[i]
			break
		}
	}
	if drama == nil {
		t.Fatal("could not find Drama Show airing")
	}
	if drama.EpisodeNumDisplay != "S02E04" {
		t.Errorf("EpisodeNumDisplay: expected %q, got %q", "S02E04", drama.EpisodeNumDisplay)
	}
	if drama.EpisodeNum != "1.3." {
		t.Errorf("EpisodeNum: expected %q, got %q", "1.3.", drama.EpisodeNum)
	}
}

func TestGetAirings_OverlapDate(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), sampleTV(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	date := time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC)
	airings, err := db.GetAirings(date)
	if err != nil {
		t.Fatalf("GetAirings: %v", err)
	}
	if len(airings) != 4 {
		t.Fatalf("expected 4 airings, got %d", len(airings))
	}
}

func TestGetAirings_ExcludesOtherDate(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), sampleTV(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	date := time.Date(2026, 3, 27, 0, 0, 0, 0, time.UTC)
	airings, err := db.GetAirings(date)
	if err != nil {
		t.Fatalf("GetAirings: %v", err)
	}
	if len(airings) != 0 {
		t.Fatalf("expected 0 airings, got %d", len(airings))
	}
}

func TestGetAirings_FieldMapping(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), sampleTV(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	date := time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC)
	airings, err := db.GetAirings(date)
	if err != nil {
		t.Fatalf("GetAirings: %v", err)
	}
	var sunrise *database.Airing
	for i := range airings {
		if airings[i].Title == "Sunrise" {
			sunrise = &airings[i]
			break
		}
	}
	if sunrise == nil {
		t.Fatal("could not find Sunrise airing")
	}
	if !sunrise.IsRepeat {
		t.Error("expected IsRepeat=true for Sunrise")
	}
	if sunrise.EpisodeNum != "5.12.0/1" {
		t.Errorf("EpisodeNum: expected %q, got %q", "5.12.0/1", sunrise.EpisodeNum)
	}
	if sunrise.EpisodeNumDisplay != "S06 E13" {
		t.Errorf("EpisodeNumDisplay: expected %q, got %q", "S06 E13", sunrise.EpisodeNumDisplay)
	}
	if sunrise.StarRating != "3.5/5" {
		t.Errorf("StarRating: expected %q, got %q", "3.5/5", sunrise.StarRating)
	}
	if sunrise.SubTitle != "Monday Edition" {
		t.Errorf("SubTitle: expected %q, got %q", "Monday Edition", sunrise.SubTitle)
	}
}

func TestGetAirings_PremiereFlagAndCategories(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), sampleTV(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	date := time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC)
	airings, err := db.GetAirings(date)
	if err != nil {
		t.Fatalf("GetAirings: %v", err)
	}
	var worldNews *database.Airing
	for i := range airings {
		if airings[i].Title == "World News" {
			worldNews = &airings[i]
			break
		}
	}
	if worldNews == nil {
		t.Fatal("could not find World News airing")
	}
	if !worldNews.IsPremiere {
		t.Error("expected IsPremiere=true for World News")
	}
	if len(worldNews.Categories) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(worldNews.Categories))
	}
	hasNews := false
	hasInternational := false
	for _, c := range worldNews.Categories {
		if c == "News" {
			hasNews = true
		}
		if c == "International" {
			hasInternational = true
		}
	}
	if !hasNews {
		t.Error("expected category 'News'")
	}
	if !hasInternational {
		t.Error("expected category 'International'")
	}
}

func TestRefresh_Upsert_NoDuplicates(t *testing.T) {
	db := openTestDB(t)
	tv := sampleTV()
	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	if err := db.Refresh(context.Background(), tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("second Refresh: %v", err)
	}
	date := time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC)
	airings, err := db.GetAirings(date)
	if err != nil {
		t.Fatalf("GetAirings: %v", err)
	}
	if len(airings) != 4 {
		t.Fatalf("expected 4 airings after double refresh, got %d", len(airings))
	}
}
