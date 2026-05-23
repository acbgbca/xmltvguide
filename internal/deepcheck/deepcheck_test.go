package deepcheck

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/acbgbca/xmltvguide/internal/plex"
)

// fakeDB implements DBProbe for tests. Behaviour is configured per-test.
type fakeDB struct {
	pingErr     error
	deepResults DBCheckResults
}

func (f *fakeDB) Ping(ctx context.Context) error               { return f.pingErr }
func (f *fakeDB) DeepCheck(ctx context.Context) DBCheckResults { return f.deepResults }

// healthyDB returns a fakeDB with all DB-related checks succeeding.
func healthyDB(now time.Time) *fakeDB {
	return &fakeDB{
		deepResults: DBCheckResults{
			ChannelCount: 5,
			AiringCount:  100,
			LastRefresh:  now.Add(-1 * time.Hour),
		},
	}
}

// successHEADServer returns 200 for HEAD requests.
func successHEADServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newChecker(t *testing.T, db DBProbe, xmltvURL string, now time.Time) *Checker {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "tvguide.db")
	imgDir := filepath.Join(dir, "images")
	if err := os.MkdirAll(filepath.Join(imgDir, "channels"), 0o750); err != nil {
		t.Fatalf("mkdir imgDir: %v", err)
	}
	return &Checker{
		DB:            db,
		HTTPClient:    http.DefaultClient,
		XMLTVURL:      xmltvURL,
		PollInterval:  12 * time.Hour,
		DBPath:        dbPath,
		ImageCacheDir: imgDir,
		Now:           func() time.Time { return now },
	}
}

func findCheck(t *testing.T, r Report, name string) CheckResult {
	t.Helper()
	for _, c := range r.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("check %q not found in report; checks=%+v", name, r.Checks)
	return CheckResult{}
}

func TestRun_AllSuccess(t *testing.T) {
	now := time.Now()
	srv := successHEADServer(t)
	c := newChecker(t, healthyDB(now), srv.URL, now)

	rep := c.Run(context.Background())

	if rep.Status != StatusSuccess {
		t.Errorf("global status = %q, want SUCCESS; report=%+v", rep.Status, rep)
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
	if len(rep.Checks) != len(wantOrder) {
		t.Fatalf("got %d checks, want %d (%v)", len(rep.Checks), len(wantOrder), rep.Checks)
	}
	for i, want := range wantOrder {
		if rep.Checks[i].Name != want {
			t.Errorf("checks[%d].Name = %q, want %q", i, rep.Checks[i].Name, want)
		}
		if rep.Checks[i].Status != StatusSuccess {
			t.Errorf("checks[%d] %s status = %q, want SUCCESS (error=%q)",
				i, rep.Checks[i].Name, rep.Checks[i].Status, rep.Checks[i].Error)
		}
	}
}

func TestRun_PingFailure_DatabaseFails(t *testing.T) {
	now := time.Now()
	db := healthyDB(now)
	db.pingErr = errors.New("boom")
	srv := successHEADServer(t)
	c := newChecker(t, db, srv.URL, now)

	rep := c.Run(context.Background())
	check := findCheck(t, rep, "database")
	if check.Status != StatusFailure {
		t.Errorf("database status = %q, want FAILURE", check.Status)
	}
	if !strings.Contains(check.Error, "boom") {
		t.Errorf("database error = %q, want to contain %q", check.Error, "boom")
	}
	if rep.Status != StatusFailure {
		t.Errorf("global status = %q, want FAILURE", rep.Status)
	}
}

func TestRun_WritableErr_DatabaseWritableFails(t *testing.T) {
	now := time.Now()
	db := healthyDB(now)
	db.deepResults.WritableErr = errors.New("readonly")
	srv := successHEADServer(t)
	c := newChecker(t, db, srv.URL, now)

	rep := c.Run(context.Background())
	check := findCheck(t, rep, "database_writable")
	if check.Status != StatusFailure {
		t.Errorf("database_writable status = %q, want FAILURE", check.Status)
	}
	if !strings.Contains(check.Error, "readonly") {
		t.Errorf("database_writable error = %q", check.Error)
	}
}

func TestRun_FTSErr_FTSFails(t *testing.T) {
	now := time.Now()
	db := healthyDB(now)
	db.deepResults.FTSErr = errors.New("no fts")
	srv := successHEADServer(t)
	c := newChecker(t, db, srv.URL, now)

	rep := c.Run(context.Background())
	check := findCheck(t, rep, "fts")
	if check.Status != StatusFailure {
		t.Errorf("fts status = %q, want FAILURE", check.Status)
	}
}

func TestRun_DataPresence_FailsOnZeroChannels(t *testing.T) {
	now := time.Now()
	db := healthyDB(now)
	db.deepResults.ChannelCount = 0
	srv := successHEADServer(t)
	c := newChecker(t, db, srv.URL, now)

	rep := c.Run(context.Background())
	check := findCheck(t, rep, "data_presence")
	if check.Status != StatusFailure {
		t.Errorf("data_presence status = %q, want FAILURE", check.Status)
	}
}

func TestRun_DataPresence_FailsOnZeroAirings(t *testing.T) {
	now := time.Now()
	db := healthyDB(now)
	db.deepResults.AiringCount = 0
	srv := successHEADServer(t)
	c := newChecker(t, db, srv.URL, now)

	rep := c.Run(context.Background())
	check := findCheck(t, rep, "data_presence")
	if check.Status != StatusFailure {
		t.Errorf("data_presence status = %q, want FAILURE", check.Status)
	}
}

func TestRun_DataPresence_FailsOnCountError(t *testing.T) {
	now := time.Now()
	db := healthyDB(now)
	db.deepResults.ChannelCountErr = errors.New("query fail")
	srv := successHEADServer(t)
	c := newChecker(t, db, srv.URL, now)

	rep := c.Run(context.Background())
	check := findCheck(t, rep, "data_presence")
	if check.Status != StatusFailure {
		t.Errorf("data_presence status = %q, want FAILURE", check.Status)
	}
}

func TestRun_DataFreshness_FailsOnZeroLastRefresh(t *testing.T) {
	now := time.Now()
	db := healthyDB(now)
	db.deepResults.LastRefresh = time.Time{}
	srv := successHEADServer(t)
	c := newChecker(t, db, srv.URL, now)

	rep := c.Run(context.Background())
	check := findCheck(t, rep, "data_freshness")
	if check.Status != StatusFailure {
		t.Errorf("data_freshness status = %q, want FAILURE", check.Status)
	}
}

func TestRun_DataFreshness_BoundaryAtTwiceInterval(t *testing.T) {
	now := time.Now()
	srv := successHEADServer(t)

	// pollInterval = 12h, so threshold is 24h.
	// just within (23h ago) → SUCCESS
	db := healthyDB(now)
	db.deepResults.LastRefresh = now.Add(-23 * time.Hour)
	c := newChecker(t, db, srv.URL, now)
	rep := c.Run(context.Background())
	if findCheck(t, rep, "data_freshness").Status != StatusSuccess {
		t.Errorf("23h-old refresh: data_freshness should be SUCCESS")
	}

	// just outside (25h ago) → FAILURE
	db2 := healthyDB(now)
	db2.deepResults.LastRefresh = now.Add(-25 * time.Hour)
	c2 := newChecker(t, db2, srv.URL, now)
	rep2 := c2.Run(context.Background())
	if findCheck(t, rep2, "data_freshness").Status != StatusFailure {
		t.Errorf("25h-old refresh: data_freshness should be FAILURE")
	}
}

func TestRun_XMLTVURL_SuccessOnHEAD200(t *testing.T) {
	now := time.Now()
	srv := successHEADServer(t)
	c := newChecker(t, healthyDB(now), srv.URL, now)

	rep := c.Run(context.Background())
	check := findCheck(t, rep, "xmltv_url")
	if check.Status != StatusSuccess {
		t.Errorf("xmltv_url status = %q, want SUCCESS (err=%q)", check.Status, check.Error)
	}
}

// TestRun_XMLTVURL_FallbackToGETOnHEADFailure verifies that any non-2xx/3xx
// HEAD response triggers the cheap Range GET fallback. The list covers the
// common rejection codes seen in the wild (405 Method Not Allowed, 406 Not
// Acceptable from xmltv.net — issue #270 — plus other typical rejections).
func TestRun_XMLTVURL_FallbackToGETOnHEADFailure(t *testing.T) {
	cases := []struct {
		name       string
		headStatus int
	}{
		{"400", http.StatusBadRequest},
		{"403", http.StatusForbidden},
		{"405", http.StatusMethodNotAllowed},
		{"406", http.StatusNotAcceptable},
		{"501", http.StatusNotImplemented},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			var headCalls, getCalls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodHead:
					headCalls.Add(1)
					w.WriteHeader(tc.headStatus)
				case http.MethodGet:
					getCalls.Add(1)
					if r.Header.Get("Range") == "" {
						t.Errorf("GET fallback should set Range header")
					}
					w.WriteHeader(http.StatusPartialContent)
					w.Write([]byte("x"))
				default:
					w.WriteHeader(http.StatusMethodNotAllowed)
				}
			}))
			t.Cleanup(srv.Close)

			c := newChecker(t, healthyDB(now), srv.URL, now)
			rep := c.Run(context.Background())
			check := findCheck(t, rep, "xmltv_url")
			if check.Status != StatusSuccess {
				t.Errorf("xmltv_url status = %q, want SUCCESS (err=%q)", check.Status, check.Error)
			}
			if headCalls.Load() != 1 {
				t.Errorf("HEAD called %d times, want 1", headCalls.Load())
			}
			if getCalls.Load() != 1 {
				t.Errorf("GET called %d times, want 1", getCalls.Load())
			}
		})
	}
}

func TestRun_XMLTVURL_SendsExpectedHeaders(t *testing.T) {
	// Both HEAD and the GET fallback must send the same Accept and User-Agent
	// headers as the real XMLTV fetcher (internal/xmltv/parser.go), otherwise
	// upstream servers may reject the request with 406 Not Acceptable.
	now := time.Now()
	wantAccept := "text/xml, application/xml, */*"
	wantUA := "xmltvguide/1.0"

	type seen struct {
		accept    string
		userAgent string
	}
	var headSeen, getSeen seen

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			headSeen = seen{accept: r.Header.Get("Accept"), userAgent: r.Header.Get("User-Agent")}
			// Force the GET fallback so we can inspect both requests.
			w.WriteHeader(http.StatusNotAcceptable)
		case http.MethodGet:
			getSeen = seen{accept: r.Header.Get("Accept"), userAgent: r.Header.Get("User-Agent")}
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte("x"))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)

	c := newChecker(t, healthyDB(now), srv.URL, now)
	rep := c.Run(context.Background())
	check := findCheck(t, rep, "xmltv_url")
	if check.Status != StatusSuccess {
		t.Fatalf("xmltv_url status = %q, want SUCCESS (err=%q)", check.Status, check.Error)
	}

	if headSeen.accept != wantAccept {
		t.Errorf("HEAD Accept = %q, want %q", headSeen.accept, wantAccept)
	}
	if headSeen.userAgent != wantUA {
		t.Errorf("HEAD User-Agent = %q, want %q", headSeen.userAgent, wantUA)
	}
	if getSeen.accept != wantAccept {
		t.Errorf("GET Accept = %q, want %q", getSeen.accept, wantAccept)
	}
	if getSeen.userAgent != wantUA {
		t.Errorf("GET User-Agent = %q, want %q", getSeen.userAgent, wantUA)
	}
}

func TestRun_XMLTVURL_FailureOn500(t *testing.T) {
	now := time.Now()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := newChecker(t, healthyDB(now), srv.URL, now)
	rep := c.Run(context.Background())
	check := findCheck(t, rep, "xmltv_url")
	if check.Status != StatusFailure {
		t.Errorf("xmltv_url status = %q, want FAILURE", check.Status)
	}
}

func TestRun_XMLTVURL_FailureOnConnRefused(t *testing.T) {
	now := time.Now()
	// Use a URL we know cannot connect.
	c := newChecker(t, healthyDB(now), "http://127.0.0.1:1/", now)
	rep := c.Run(context.Background())
	check := findCheck(t, rep, "xmltv_url")
	if check.Status != StatusFailure {
		t.Errorf("xmltv_url status = %q, want FAILURE", check.Status)
	}
	if check.Error == "" {
		t.Errorf("xmltv_url should have an error message on failure")
	}
}

func TestRun_DiskData_ReportsModeAndUID(t *testing.T) {
	now := time.Now()
	srv := successHEADServer(t)
	c := newChecker(t, healthyDB(now), srv.URL, now)

	rep := c.Run(context.Background())
	check := findCheck(t, rep, "disk_data")
	if check.Status != StatusSuccess {
		t.Errorf("disk_data status = %q, want SUCCESS (err=%q)", check.Status, check.Error)
	}
	if !strings.Contains(check.Info, "mode=") || !strings.Contains(check.Info, "uid=") {
		t.Errorf("disk_data info = %q, want mode=... uid=...", check.Info)
	}
}

func TestRun_DiskData_FailsWhenParentMissing(t *testing.T) {
	now := time.Now()
	srv := successHEADServer(t)
	c := newChecker(t, healthyDB(now), srv.URL, now)
	// Point DBPath to a non-existent directory.
	c.DBPath = filepath.Join(t.TempDir(), "does-not-exist", "tvguide.db")

	rep := c.Run(context.Background())
	check := findCheck(t, rep, "disk_data")
	if check.Status != StatusFailure {
		t.Errorf("disk_data status = %q, want FAILURE", check.Status)
	}
}

func TestRun_ImageCache_FailsWhenDirMissing(t *testing.T) {
	now := time.Now()
	srv := successHEADServer(t)
	c := newChecker(t, healthyDB(now), srv.URL, now)
	c.ImageCacheDir = filepath.Join(t.TempDir(), "no-such-dir")

	rep := c.Run(context.Background())
	check := findCheck(t, rep, "image_cache")
	if check.Status != StatusFailure {
		t.Errorf("image_cache status = %q, want FAILURE", check.Status)
	}
}

func TestRun_AllChecksRun_EvenWhenOneFails(t *testing.T) {
	// One failure should not short-circuit subsequent checks.
	now := time.Now()
	db := healthyDB(now)
	db.deepResults.FTSErr = errors.New("no fts")
	srv := successHEADServer(t)
	c := newChecker(t, db, srv.URL, now)

	rep := c.Run(context.Background())
	if len(rep.Checks) != 10 {
		t.Fatalf("expected 10 checks, got %d (%+v)", len(rep.Checks), rep.Checks)
	}
	// All non-fts checks should still be SUCCESS.
	for _, c := range rep.Checks {
		if c.Name == "fts" {
			if c.Status != StatusFailure {
				t.Errorf("fts should be FAILURE")
			}
		} else {
			if c.Status != StatusSuccess {
				t.Errorf("%s status = %q, want SUCCESS (err=%q)", c.Name, c.Status, c.Error)
			}
		}
	}
	if rep.Status != StatusFailure {
		t.Errorf("global status should be FAILURE when any check fails")
	}
}

func TestRun_ErrorMessageContainsURLPrefix(t *testing.T) {
	// Errors should be wrapped with a useful prefix.
	now := time.Now()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	c := newChecker(t, healthyDB(now), srv.URL, now)
	rep := c.Run(context.Background())
	check := findCheck(t, rep, "xmltv_url")
	if !strings.Contains(check.Error, srv.URL) {
		t.Errorf("xmltv_url error %q should reference URL %q", check.Error, srv.URL)
	}
}

// TestRun_XMLTVURL_RespectsPerRequestTimeout uses a server that never responds
// to verify the per-request 5s timeout is honoured (we set Now to make tests
// run quickly using a Checker-internal short timeout — the implementation uses
// 5s so we cap our wait at 8s).
func TestRun_XMLTVURL_RespectsPerRequestTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("slow test")
	}
	now := time.Now()
	// Use a TCP listener that accepts but never responds.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(20 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)

	c := newChecker(t, healthyDB(now), srv.URL, now)

	start := time.Now()
	rep := c.Run(context.Background())
	elapsed := time.Since(start)

	check := findCheck(t, rep, "xmltv_url")
	if check.Status != StatusFailure {
		t.Errorf("xmltv_url status = %q, want FAILURE (timeout)", check.Status)
	}
	if elapsed > 10*time.Second {
		t.Errorf("xmltv_url took %s, expected ~5s timeout", elapsed)
	}
}

// Compile-time guard that fakeDB implements DBProbe.
var _ DBProbe = (*fakeDB)(nil)

// fakePlex implements PlexProbe for tests.
type fakePlex struct {
	pingErr error
}

func (f *fakePlex) Ping(ctx context.Context) error { return f.pingErr }

// Compile-time guard that fakePlex implements PlexProbe.
var _ PlexProbe = (*fakePlex)(nil)

func TestRun_PlexReachable_NotConfigured(t *testing.T) {
	now := time.Now()
	srv := successHEADServer(t)
	c := newChecker(t, healthyDB(now), srv.URL, now)
	c.PlexURL = ""
	c.PlexClient = nil

	rep := c.Run(context.Background())
	check := findCheck(t, rep, "plex_reachable")
	if check.Status != StatusSuccess {
		t.Errorf("plex_reachable status = %q, want SUCCESS (err=%q)", check.Status, check.Error)
	}
	if !strings.Contains(strings.ToLower(check.Info), "not configured") {
		t.Errorf("plex_reachable info = %q, want to mention 'not configured'", check.Info)
	}
	// Not-configured plex must not fail the overall report.
	if rep.Status != StatusSuccess {
		t.Errorf("global status = %q, want SUCCESS when only plex is unset", rep.Status)
	}
}

func TestRun_PlexReachable_Healthy(t *testing.T) {
	now := time.Now()
	srv := successHEADServer(t)
	c := newChecker(t, healthyDB(now), srv.URL, now)
	c.PlexURL = "http://plex.local:32400"
	c.PlexClient = &fakePlex{pingErr: nil}

	rep := c.Run(context.Background())
	check := findCheck(t, rep, "plex_reachable")
	if check.Status != StatusSuccess {
		t.Errorf("plex_reachable status = %q, want SUCCESS (err=%q)", check.Status, check.Error)
	}
}

func TestRun_PlexReachable_UnauthorizedReportsActionableMessage(t *testing.T) {
	now := time.Now()
	srv := successHEADServer(t)
	c := newChecker(t, healthyDB(now), srv.URL, now)
	c.PlexURL = "http://plex.local:32400"
	c.PlexClient = &fakePlex{pingErr: plex.ErrUnauthorized}

	rep := c.Run(context.Background())
	check := findCheck(t, rep, "plex_reachable")
	if check.Status != StatusFailure {
		t.Errorf("plex_reachable status = %q, want FAILURE", check.Status)
	}
	if !strings.Contains(check.Error, "PLEX_TOKEN") {
		t.Errorf("plex_reachable error = %q, want to mention PLEX_TOKEN", check.Error)
	}
	if rep.Status != StatusFailure {
		t.Errorf("global status should be FAILURE when plex auth fails")
	}
}

func TestRun_PlexReachable_GenericError(t *testing.T) {
	now := time.Now()
	srv := successHEADServer(t)
	c := newChecker(t, healthyDB(now), srv.URL, now)
	c.PlexURL = "http://plex.local:32400"
	c.PlexClient = &fakePlex{pingErr: errors.New("connection refused")}

	rep := c.Run(context.Background())
	check := findCheck(t, rep, "plex_reachable")
	if check.Status != StatusFailure {
		t.Errorf("plex_reachable status = %q, want FAILURE", check.Status)
	}
	if !strings.Contains(check.Error, "connection refused") {
		t.Errorf("plex_reachable error = %q, want the underlying error", check.Error)
	}
}
