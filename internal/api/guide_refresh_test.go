package api_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/acbgbca/xmltvguide/internal/api"
	"github.com/acbgbca/xmltvguide/internal/database"
	"github.com/acbgbca/xmltvguide/internal/images"
)

// newRefreshServer creates a test API server with a controllable refreshFn.
func newRefreshServer(t *testing.T, refreshFn func() error) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test-source", images.NewCache(&http.Client{}, filepath.Join(dir, "images")), nil, nil, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	h := api.New(db, 0, refreshFn)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestPostGuideRefresh_Async(t *testing.T) {
	var called atomic.Bool
	done := make(chan struct{})
	srv := newRefreshServer(t, func() error {
		called.Store(true)
		close(done)
		return nil
	})

	resp, err := httpPost(t, srv.URL+"/api/guide/refresh", nil)
	if err != nil {
		t.Fatalf("POST /api/guide/refresh: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("expected 202 Accepted, got %d", resp.StatusCode)
	}

	// Wait for the async goroutine to run.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("refresh function was not called within timeout")
	}
	if !called.Load() {
		t.Error("expected refreshFn to be called")
	}
}

func TestPostGuideRefresh_Sync_Success(t *testing.T) {
	var called atomic.Bool
	srv := newRefreshServer(t, func() error {
		called.Store(true)
		return nil
	})

	resp, err := httpPost(t, srv.URL+"/api/guide/refresh?sync=true", nil)
	if err != nil {
		t.Fatalf("POST /api/guide/refresh?sync=true: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["ok"] != true {
		t.Errorf("expected ok=true, got %v", body["ok"])
	}
	if !called.Load() {
		t.Error("expected refreshFn to be called")
	}
}

func TestPostGuideRefresh_Sync_Error(t *testing.T) {
	srv := newRefreshServer(t, func() error {
		return errors.New("xmltv fetch failed")
	})

	resp, err := httpPost(t, srv.URL+"/api/guide/refresh?sync=true", nil)
	if err != nil {
		t.Fatalf("POST /api/guide/refresh?sync=true: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error"] != "xmltv fetch failed" {
		t.Errorf("expected error message, got %v", body["error"])
	}
}

func TestPostGuideRefresh_NoRefreshFn(t *testing.T) {
	// When no refreshFn is configured, endpoint should return 501 Not Implemented.
	srv := newRefreshServer(t, nil)

	resp, err := httpPost(t, srv.URL+"/api/guide/refresh", nil)
	if err != nil {
		t.Fatalf("POST /api/guide/refresh: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("expected 501 Not Implemented, got %d", resp.StatusCode)
	}
}
