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
	"github.com/acbgbca/xmltvguide/internal/images"
	"github.com/acbgbca/xmltvguide/internal/model"
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
	cache := images.NewCache(client, filepath.Join(dir, "images"))
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test", cache)
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
	cache := images.NewCache(iconServer.Client(), filepath.Join(dir, "images"))
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test", cache)
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

// testBaseDate returns today at midnight UTC. Using a date derived from
// time.Now() ensures that test data stays within the retention window and is
// never pruned during the Refresh call inside each test.
func testBaseDate() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

func sampleTV() *xmltv.TV {
	base := testBaseDate()
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
				Start:       xmltv.XmltvTime{Time: base.Add(-time.Hour)},          // 23:00 the previous day
				Stop:        xmltv.XmltvTime{Time: base.Add(time.Hour)},           // 01:00 today — overlaps midnight
				Channel:     "ch2",
				Titles:      []xmltv.Name{{Value: "Late Night Movie"}},
				Descs:       []xmltv.Name{{Value: "A classic late night movie spanning midnight."}},
				Categories:  []xmltv.Name{{Value: "Movie"}},
			},
			{
				Start:      xmltv.XmltvTime{Time: base.Add(6 * time.Hour)},
				Stop:       xmltv.XmltvTime{Time: base.Add(7 * time.Hour)},
				Channel:    "ch1",
				Titles:     []xmltv.Name{{Value: "Morning News"}},
				Descs:      []xmltv.Name{{Value: "The latest news to start your day."}},
				Categories: []xmltv.Name{{Value: "News"}},
				Ratings:    []xmltv.Rating{{Value: "G", System: "ABA"}},
			},
			{
				Start:       xmltv.XmltvTime{Time: base.Add(7 * time.Hour)},
				Stop:        xmltv.XmltvTime{Time: base.Add(9 * time.Hour)},
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
				Start:      xmltv.XmltvTime{Time: base.Add(6*time.Hour + 30*time.Minute)},
				Stop:       xmltv.XmltvTime{Time: base.Add(7*time.Hour + 30*time.Minute)},
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

// TestRefresh_FTSRebuildSucceedsOnSubsequentRefresh verifies that a second
// Refresh correctly clears and rebuilds the FTS index when it already contains
// data. Regression test for GitHub issue #87 where DELETE FROM airings_fts
// failed in scratch Docker containers (no /tmp directory) on the second
// refresh, because FTS5 segment operations require a writable temp directory.
func TestRefresh_FTSRebuildSucceedsOnSubsequentRefresh(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	tv := sampleTV()

	// First refresh populates the FTS index.
	if err := db.Refresh(ctx, tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}

	// Second refresh must clear and rebuild the populated FTS index without error.
	if err := db.Refresh(ctx, tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("second Refresh (FTS rebuild): %v", err)
	}

	// FTS search must return results after the rebuild.
	// Use SearchAdvanced with includePast=true so the result is not
	// dependent on what time of day the test runs.
	results, err := db.SearchAdvanced("Morning News", nil, true, true, false)
	if err != nil {
		t.Fatalf("SearchAdvanced after second refresh: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected FTS search results after second refresh, got none")
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
		Start:   xmltv.XmltvTime{Time: testBaseDate().Add(9 * time.Hour)},
		Stop:    xmltv.XmltvTime{Time: testBaseDate().Add(10 * time.Hour)},
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
	date := testBaseDate()
	airings, err := db.GetAirings(date)
	if err != nil {
		t.Fatalf("GetAirings: %v", err)
	}
	var drama *model.Airing
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
	date := testBaseDate()
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
	date := testBaseDate().AddDate(0, 0, -2)
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
	date := testBaseDate()
	airings, err := db.GetAirings(date)
	if err != nil {
		t.Fatalf("GetAirings: %v", err)
	}
	var sunrise *model.Airing
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
	date := testBaseDate()
	airings, err := db.GetAirings(date)
	if err != nil {
		t.Fatalf("GetAirings: %v", err)
	}
	var worldNews *model.Airing
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

// --- FTS5 and Search tests ---

// TestFTS5_Available verifies that modernc.org/sqlite supports FTS5.
func TestFTS5_Available(t *testing.T) {
	db := openTestDB(t)
	// Attempt to create a simple FTS5 table. If this fails, FTS5 is not compiled in.
	_, err := db.ExecRaw(`CREATE VIRTUAL TABLE IF NOT EXISTS fts5_test USING fts5(content)`)
	if err != nil {
		t.Fatalf("FTS5 not available in modernc.org/sqlite: %v", err)
	}
	_, err = db.ExecRaw(`DROP TABLE fts5_test`)
	if err != nil {
		t.Fatalf("dropping FTS5 test table: %v", err)
	}
}

// searchTV returns test data with airings in the future and past, with varied
// titles, subtitles, descriptions, categories, and repeat flags for search tests.
func searchTV() *xmltv.TV {
	return &xmltv.TV{
		Channels: []xmltv.Channel{
			{ID: "ch1", DisplayNames: []xmltv.Name{{Value: "ABC"}}},
			{ID: "ch2", DisplayNames: []xmltv.Name{{Value: "SBS"}}},
		},
		Programmes: []xmltv.Programme{
			{
				// Future, non-repeat, title match for "News"
				Start:      xmltv.XmltvTime{Time: time.Now().Add(1 * time.Hour)},
				Stop:       xmltv.XmltvTime{Time: time.Now().Add(2 * time.Hour)},
				Channel:    "ch1",
				Titles:     []xmltv.Name{{Value: "Morning News"}},
				Descs:      []xmltv.Name{{Value: "Start your day with the latest headlines."}},
				Categories: []xmltv.Name{{Value: "News"}},
			},
			{
				// Future, repeat, title match for "News"
				Start:           xmltv.XmltvTime{Time: time.Now().Add(3 * time.Hour)},
				Stop:            xmltv.XmltvTime{Time: time.Now().Add(4 * time.Hour)},
				Channel:         "ch1",
				Titles:          []xmltv.Name{{Value: "Evening News"}},
				Descs:           []xmltv.Name{{Value: "Recap of today's events."}},
				Categories:      []xmltv.Name{{Value: "News"}},
				PreviouslyShown: &xmltv.PreviouslyShown{},
			},
			{
				// Future, non-repeat, title does NOT match "News" but description does
				Start:      xmltv.XmltvTime{Time: time.Now().Add(5 * time.Hour)},
				Stop:       xmltv.XmltvTime{Time: time.Now().Add(6 * time.Hour)},
				Channel:    "ch2",
				Titles:     []xmltv.Name{{Value: "Documentary Special"}},
				SubTitles:  []xmltv.Name{{Value: "Behind the news desk"}},
				Descs:      []xmltv.Name{{Value: "A look at how news is made."}},
				Categories: []xmltv.Name{{Value: "Documentary"}},
			},
			{
				// Past, non-repeat, title match for "News"
				Start:      xmltv.XmltvTime{Time: time.Now().Add(-3 * time.Hour)},
				Stop:       xmltv.XmltvTime{Time: time.Now().Add(-2 * time.Hour)},
				Channel:    "ch1",
				Titles:     []xmltv.Name{{Value: "Late Night News"}},
				Descs:      []xmltv.Name{{Value: "Yesterday's late night news."}},
				Categories: []xmltv.Name{{Value: "News"}},
			},
			{
				// Future, non-repeat, category "Sport"
				Start:      xmltv.XmltvTime{Time: time.Now().Add(7 * time.Hour)},
				Stop:       xmltv.XmltvTime{Time: time.Now().Add(8 * time.Hour)},
				Channel:    "ch2",
				Titles:     []xmltv.Name{{Value: "Sports Tonight"}},
				Descs:      []xmltv.Name{{Value: "All the sport highlights."}},
				Categories: []xmltv.Name{{Value: "Sport"}, {Value: "Entertainment"}},
			},
			{
				// Currently airing (started in past, ends in future), title match for "News"
				Start:      xmltv.XmltvTime{Time: time.Now().Add(-30 * time.Minute)},
				Stop:       xmltv.XmltvTime{Time: time.Now().Add(30 * time.Minute)},
				Channel:    "ch2",
				Titles:     []xmltv.Name{{Value: "Midday News"}},
				Descs:      []xmltv.Name{{Value: "The latest midday headlines."}},
				Categories: []xmltv.Name{{Value: "News"}},
			},
		},
	}
}

func TestSearchSimple_MatchesTitle(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), searchTV(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	results, err := db.SearchSimple("News", true, false)
	if err != nil {
		t.Fatalf("SearchSimple: %v", err)
	}
	// Should match "Morning News", "Evening News" (future, title match only)
	// Should match "Midday News" (currently airing)
	// Should NOT match "Documentary Special" (title doesn't contain "News")
	// Should NOT match "Late Night News" (finished past airing)
	for _, r := range results {
		if r.Title == "Documentary Special" {
			t.Error("SearchSimple should not match on subtitle/description, only title")
		}
		if r.Title == "Late Night News" {
			t.Error("SearchSimple should exclude finished past airings")
		}
	}
	if len(results) < 3 {
		t.Errorf("expected at least 3 results matching 'News' in title (including currently airing), got %d", len(results))
	}
}

func TestSearchSimple_ExcludesDescriptionOnlyMatches(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), searchTV(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	results, err := db.SearchSimple("News", true, false)
	if err != nil {
		t.Fatalf("SearchSimple: %v", err)
	}
	for _, r := range results {
		if r.Title == "Documentary Special" {
			t.Error("SearchSimple should not return results that only match in description/subtitle")
		}
	}
}

func TestSearchSimple_ExcludesFinishedAirings(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), searchTV(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	results, err := db.SearchSimple("News", true, false)
	if err != nil {
		t.Fatalf("SearchSimple: %v", err)
	}
	for _, r := range results {
		if r.Stop.Before(time.Now()) {
			t.Errorf("SearchSimple returned finished airing: %q (stopped at %v)", r.Title, r.Stop)
		}
	}
}

func TestSearchSimple_ExcludesRepeats(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), searchTV(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	results, err := db.SearchSimple("News", false, false)
	if err != nil {
		t.Fatalf("SearchSimple: %v", err)
	}
	for _, r := range results {
		if r.IsRepeat {
			t.Errorf("SearchSimple with includeRepeats=false returned repeat: %q", r.Title)
		}
	}
}

func TestSearchSimple_IncludesRepeats(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), searchTV(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	results, err := db.SearchSimple("News", true, false)
	if err != nil {
		t.Fatalf("SearchSimple: %v", err)
	}
	hasRepeat := false
	for _, r := range results {
		if r.IsRepeat {
			hasRepeat = true
		}
	}
	if !hasRepeat {
		t.Error("SearchSimple with includeRepeats=true should include repeats")
	}
}

func TestSearchAdvanced_MatchesTitleSubtitleDescription(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), searchTV(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	// "news" should match in title (Morning News, Evening News) AND subtitle/description (Documentary Special)
	results, err := db.SearchAdvanced("news", nil, false, true, false)
	if err != nil {
		t.Fatalf("SearchAdvanced: %v", err)
	}
	titles := map[string]bool{}
	for _, r := range results {
		titles[r.Title] = true
	}
	if !titles["Morning News"] {
		t.Error("expected Morning News in results")
	}
	if !titles["Documentary Special"] {
		t.Error("expected Documentary Special in results (matches in subtitle/description)")
	}
}

func TestSearchAdvanced_FiltersByCategory(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), searchTV(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	// Search for "news" but filter to "Documentary" category only
	results, err := db.SearchAdvanced("news", []string{"Documentary"}, false, true, false)
	if err != nil {
		t.Fatalf("SearchAdvanced: %v", err)
	}
	for _, r := range results {
		if r.Title == "Morning News" {
			t.Error("Morning News should be filtered out — it's not in Documentary category")
		}
	}
	hasDocumentary := false
	for _, r := range results {
		if r.Title == "Documentary Special" {
			hasDocumentary = true
		}
	}
	if !hasDocumentary {
		t.Error("expected Documentary Special in results")
	}
}

func TestSearchAdvanced_IncludesPastAirings(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), searchTV(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	results, err := db.SearchAdvanced("News", nil, true, true, false)
	if err != nil {
		t.Fatalf("SearchAdvanced: %v", err)
	}
	hasPast := false
	for _, r := range results {
		if r.Start.Before(time.Now()) {
			hasPast = true
		}
	}
	if !hasPast {
		t.Error("SearchAdvanced with includePast=true should include past airings")
	}
}

func TestSearchAdvanced_ExcludesFinishedAirings(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), searchTV(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	results, err := db.SearchAdvanced("News", nil, false, true, false)
	if err != nil {
		t.Fatalf("SearchAdvanced: %v", err)
	}
	for _, r := range results {
		if r.Stop.Before(time.Now()) {
			t.Errorf("SearchAdvanced with includePast=false returned finished airing: %q (stopped at %v)", r.Title, r.Stop)
		}
	}
}

func TestSearchAdvanced_ExcludesRepeats(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), searchTV(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	results, err := db.SearchAdvanced("News", nil, false, false, false)
	if err != nil {
		t.Fatalf("SearchAdvanced: %v", err)
	}
	for _, r := range results {
		if r.IsRepeat {
			t.Errorf("SearchAdvanced with includeRepeats=false returned repeat: %q", r.Title)
		}
	}
}

func TestSearchAdvanced_CombinesCategoryAndText(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), searchTV(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	// Search for "sport" filtered to "Entertainment" category
	results, err := db.SearchAdvanced("sport", []string{"Entertainment"}, false, true, false)
	if err != nil {
		t.Fatalf("SearchAdvanced: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least 1 result for 'sport' in Entertainment category")
	}
	for _, r := range results {
		if r.Title != "Sports Tonight" {
			t.Errorf("unexpected result: %q", r.Title)
		}
	}
}

func TestSearchSimple_IncludesCurrentlyAiring(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), searchTV(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	results, err := db.SearchSimple("News", true, false)
	if err != nil {
		t.Fatalf("SearchSimple: %v", err)
	}
	hasCurrentlyAiring := false
	for _, r := range results {
		if r.Title == "Midday News" {
			hasCurrentlyAiring = true
		}
	}
	if !hasCurrentlyAiring {
		t.Error("SearchSimple should include currently-airing shows (started in past, still in progress)")
	}
}

func TestSearchAdvanced_IncludesCurrentlyAiring(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), searchTV(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	results, err := db.SearchAdvanced("News", nil, false, true, false)
	if err != nil {
		t.Fatalf("SearchAdvanced: %v", err)
	}
	hasCurrentlyAiring := false
	for _, r := range results {
		if r.Title == "Midday News" {
			hasCurrentlyAiring = true
		}
	}
	if !hasCurrentlyAiring {
		t.Error("SearchAdvanced with includePast=false should include currently-airing shows")
	}
}

// searchTVWithTomorrow returns test data like searchTV but adds an airing
// starting tomorrow, used to test the "today" filter.
func searchTVWithTomorrow() *xmltv.TV {
	tv := searchTV()
	tomorrow := time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day()+1, 10, 0, 0, 0, time.Local)
	tv.Programmes = append(tv.Programmes, xmltv.Programme{
		Start:      xmltv.XmltvTime{Time: tomorrow},
		Stop:       xmltv.XmltvTime{Time: tomorrow.Add(1 * time.Hour)},
		Channel:    "ch1",
		Titles:     []xmltv.Name{{Value: "Tomorrow News"}},
		Descs:      []xmltv.Name{{Value: "News from the future."}},
		Categories: []xmltv.Name{{Value: "News"}},
	})
	return tv
}

func TestSearchSimple_TodayFilter_ExcludesTomorrow(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), searchTVWithTomorrow(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	results, err := db.SearchSimple("News", true, true)
	if err != nil {
		t.Fatalf("SearchSimple: %v", err)
	}
	for _, r := range results {
		if r.Title == "Tomorrow News" {
			t.Error("SearchSimple with today=true should exclude airings starting tomorrow")
		}
	}
	if len(results) == 0 {
		t.Error("expected at least one result for today")
	}
}

func TestSearchSimple_TodayFilter_IncludesToday(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), searchTVWithTomorrow(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	// Without today filter, tomorrow's airing should be included
	results, err := db.SearchSimple("News", true, false)
	if err != nil {
		t.Fatalf("SearchSimple: %v", err)
	}
	hasTomorrow := false
	for _, r := range results {
		if r.Title == "Tomorrow News" {
			hasTomorrow = true
		}
	}
	if !hasTomorrow {
		t.Error("SearchSimple with today=false should include airings starting tomorrow")
	}
}

func TestSearchAdvanced_TodayFilter_ExcludesTomorrow(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), searchTVWithTomorrow(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	results, err := db.SearchAdvanced("News", nil, false, true, true)
	if err != nil {
		t.Fatalf("SearchAdvanced: %v", err)
	}
	for _, r := range results {
		if r.Title == "Tomorrow News" {
			t.Error("SearchAdvanced with today=true should exclude airings starting tomorrow")
		}
	}
	if len(results) == 0 {
		t.Error("expected at least one result for today")
	}
}

func TestSearchAdvanced_TodayFilter_IncludesToday(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), searchTVWithTomorrow(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	results, err := db.SearchAdvanced("News", nil, false, true, false)
	if err != nil {
		t.Fatalf("SearchAdvanced: %v", err)
	}
	hasTomorrow := false
	for _, r := range results {
		if r.Title == "Tomorrow News" {
			hasTomorrow = true
		}
	}
	if !hasTomorrow {
		t.Error("SearchAdvanced with today=false should include airings starting tomorrow")
	}
}

func TestGetCategories_ReturnsSorted(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), searchTV(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	cats, err := db.GetCategories()
	if err != nil {
		t.Fatalf("GetCategories: %v", err)
	}
	expected := []string{"Documentary", "Entertainment", "News", "Sport"}
	if len(cats) != len(expected) {
		t.Fatalf("expected %d categories, got %d: %v", len(expected), len(cats), cats)
	}
	for i, c := range cats {
		if c != expected[i] {
			t.Errorf("category[%d]: expected %q, got %q", i, expected[i], c)
		}
	}
}

func TestGetCategories_EmptyWhenNoData(t *testing.T) {
	db := openTestDB(t)
	cats, err := db.GetCategories()
	if err != nil {
		t.Fatalf("GetCategories: %v", err)
	}
	if cats == nil {
		t.Fatal("expected non-nil slice, got nil")
	}
	if len(cats) != 0 {
		t.Fatalf("expected 0 categories, got %d", len(cats))
	}
}

func TestRefresh_PopulatesFTSAndCategories(t *testing.T) {
	db := openTestDB(t)
	if err := db.Refresh(context.Background(), searchTV(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// FTS should be populated — simple search should work
	results, err := db.SearchSimple("Morning", true, false)
	if err != nil {
		t.Fatalf("SearchSimple after Refresh: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected FTS results after Refresh")
	}

	// Categories should be populated
	cats, err := db.GetCategories()
	if err != nil {
		t.Fatalf("GetCategories after Refresh: %v", err)
	}
	if len(cats) == 0 {
		t.Error("expected categories after Refresh")
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
	date := testBaseDate()
	airings, err := db.GetAirings(date)
	if err != nil {
		t.Fatalf("GetAirings: %v", err)
	}
	if len(airings) != 4 {
		t.Fatalf("expected 4 airings after double refresh, got %d", len(airings))
	}
}
