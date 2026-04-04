package images_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/acbgbca/xmltvguide/internal/images"
)

func newTestCache(t *testing.T, client *http.Client) *images.Cache {
	t.Helper()
	dir := t.TempDir()
	return images.NewCache(client, dir)
}

func startIconServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("\x89PNG\r\n\x1a\n"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

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

func TestCache_Download_Success(t *testing.T) {
	srv := startIconServer(t)
	cache := newTestCache(t, srv.Client())

	path, err := cache.Download(context.Background(), "ch1", srv.URL+"/icon.png")
	if err != nil {
		t.Fatalf("Download: unexpected error: %v", err)
	}
	if path == "" {
		t.Fatal("Download: expected non-empty path")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Download: file not found at %s: %v", path, err)
	}
	if filepath.Ext(path) != ".png" {
		t.Errorf("Download: expected .png extension, got %s", filepath.Ext(path))
	}
}

func TestCache_Download_SetsRequiredHeaders(t *testing.T) {
	srv := startStrictIconServer(t)
	cache := newTestCache(t, srv.Client())

	path, err := cache.Download(context.Background(), "ch1", srv.URL+"/icon.png")
	if err != nil {
		t.Fatalf("Download: strict server rejected request (missing headers?): %v", err)
	}
	if path == "" {
		t.Fatal("Download: expected non-empty path")
	}
}

func TestCache_Download_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	cache := newTestCache(t, srv.Client())

	_, err := cache.Download(context.Background(), "ch1", srv.URL+"/icon.png")
	if err == nil {
		t.Fatal("Download: expected error for non-200 response")
	}
}

func TestCache_Download_URLExtension(t *testing.T) {
	// Server returns no Content-Type header, rely on URL extension.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data"))
	}))
	t.Cleanup(srv.Close)
	cache := newTestCache(t, srv.Client())

	path, err := cache.Download(context.Background(), "ch1", srv.URL+"/icon.jpg")
	if err != nil {
		t.Fatalf("Download: unexpected error: %v", err)
	}
	if filepath.Ext(path) != ".jpg" {
		t.Errorf("Download: expected .jpg extension from URL, got %s", filepath.Ext(path))
	}
}

func TestCache_Download_DefaultExtension(t *testing.T) {
	// Server returns no Content-Type header and URL has no recognised extension.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data"))
	}))
	t.Cleanup(srv.Close)
	cache := newTestCache(t, srv.Client())

	path, err := cache.Download(context.Background(), "ch1", srv.URL+"/icon")
	if err != nil {
		t.Fatalf("Download: unexpected error: %v", err)
	}
	if filepath.Ext(path) != ".jpg" {
		t.Errorf("Download: expected default .jpg extension, got %s", filepath.Ext(path))
	}
}

func TestCache_EnsureIcon_FileExists(t *testing.T) {
	srv := startIconServer(t)
	cache := newTestCache(t, srv.Client())

	// Pre-create a local file.
	dir := t.TempDir()
	existing := filepath.Join(dir, "ch1.png")
	if err := os.WriteFile(existing, []byte("fake png"), 0644); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	path, err := cache.EnsureIcon(context.Background(), "ch1", existing, srv.URL+"/icon.png")
	if err != nil {
		t.Fatalf("EnsureIcon: unexpected error: %v", err)
	}
	if path != existing {
		t.Errorf("EnsureIcon: expected %s, got %s", existing, path)
	}
}

func TestCache_EnsureIcon_FileMissing_Downloads(t *testing.T) {
	srv := startIconServer(t)
	cache := newTestCache(t, srv.Client())

	// Local path provided but the file doesn't exist on disk.
	missingPath := filepath.Join(t.TempDir(), "missing.png")

	path, err := cache.EnsureIcon(context.Background(), "ch1", missingPath, srv.URL+"/icon.png")
	if err != nil {
		t.Fatalf("EnsureIcon: unexpected error: %v", err)
	}
	if path == "" {
		t.Fatal("EnsureIcon: expected non-empty path after re-download")
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("EnsureIcon: downloaded file not found at %s: %v", path, statErr)
	}
}

func TestCache_EnsureIcon_EmptyLocalPath_Downloads(t *testing.T) {
	srv := startIconServer(t)
	cache := newTestCache(t, srv.Client())

	path, err := cache.EnsureIcon(context.Background(), "ch1", "", srv.URL+"/icon.png")
	if err != nil {
		t.Fatalf("EnsureIcon: unexpected error: %v", err)
	}
	if path == "" {
		t.Fatal("EnsureIcon: expected non-empty path after download")
	}
}

func TestCache_EnsureIcon_EmptyURL(t *testing.T) {
	cache := newTestCache(t, http.DefaultClient)

	path, err := cache.EnsureIcon(context.Background(), "ch1", "", "")
	if err != nil {
		t.Fatalf("EnsureIcon: unexpected error: %v", err)
	}
	if path != "" {
		t.Errorf("EnsureIcon: expected empty path for channel with no icon URL, got %s", path)
	}
}
