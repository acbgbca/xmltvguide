package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // embed IANA timezone database so the binary works on scratch

	"github.com/acbgbca/xmltvguide/internal/api"
	"github.com/acbgbca/xmltvguide/internal/database"
	"github.com/acbgbca/xmltvguide/internal/images"
	"github.com/acbgbca/xmltvguide/internal/xmltv"
)

//go:embed web
var webFS embed.FS

func main() {
	xmltvURL := os.Getenv("XMLTV_URL")
	if xmltvURL == "" {
		log.Fatal("XMLTV_URL environment variable is required")
	}

	pollInterval := 12 * time.Hour
	if v := os.Getenv("POLL_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			log.Fatalf("invalid POLL_INTERVAL %q: %v", v, err)
		}
		pollInterval = d
	}

	retentionDays := 7
	if v := os.Getenv("RETENTION_DAYS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			log.Fatalf("invalid RETENTION_DAYS %q: must be a positive integer", v)
		}
		retentionDays = n
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "/data/tvguide.db"
	}

	imageCacheDir := os.Getenv("IMAGE_CACHE_DIR")
	if imageCacheDir == "" {
		imageCacheDir = "/data/images"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	hiddenIDs, hiddenLCNs := parseHiddenChannels(os.Getenv("HIDDEN_CHANNELS"))
	stripWords := parseStripWords(os.Getenv("CHANNEL_NAME_STRIP"))

	// Ensure the database directory exists (relevant when running outside Docker).
	if err := os.MkdirAll(filepath.Dir(dbPath), 0750); err != nil {
		log.Fatalf("creating database directory %s: %v", filepath.Dir(dbPath), err)
	}

	// Ensure the image cache directory exists.
	if err := os.MkdirAll(filepath.Join(imageCacheDir, "channels"), 0750); err != nil {
		log.Fatalf("creating image cache directory %s: %v", imageCacheDir, err)
	}

	httpClient := &http.Client{Timeout: 5 * time.Minute}

	imageCache := images.NewCache(httpClient, imageCacheDir)

	db, err := database.Open(dbPath, retentionDays, xmltvURL, imageCache, hiddenIDs, hiddenLCNs, stripWords)
	if err != nil {
		log.Fatalf("opening database: %v", err)
	}

	refreshOnStart := os.Getenv("REFRESH_ON_START") == "true"
	runInitialRefresh(db, httpClient, xmltvURL, pollInterval, refreshOnStart)

	// Background refresh goroutine.
	ticker := time.NewTicker(pollInterval)
	go func() {
		defer ticker.Stop()
		for range ticker.C {
			if err := refresh(db, httpClient, xmltvURL, pollInterval); err != nil {
				log.Printf("refresh error: %v", err)
			}
		}
	}()

	mux := http.NewServeMux()

	rssTTL := 0
	if v := os.Getenv("RSS_TTL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			rssTTL = n
		}
	}

	apiHandler := api.New(db, rssTTL)
	apiHandler.RegisterRoutes(mux)

	webContent, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("embedding web files: %v", err)
	}
	mux.Handle("/", spaHandler(http.FS(webContent)))

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	log.Printf("TV Guide starting on :%s (poll: %s, retention: %d days, db: %s)",
		port, pollInterval, retentionDays, dbPath)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down...")

	ticker.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("forced shutdown: %v", err)
	}
	_ = db.Close()
}

func runInitialRefresh(db *database.DB, client *http.Client, xmltvURL string, pollInterval time.Duration, refreshOnStart bool) {
	// Perform initial data fetch only when explicitly requested or when the
	// database is empty (fresh install). Otherwise schedule the first refresh
	// at the normal poll interval so restarts don't hammer the XMLTV source.
	if refreshOnStart || !db.HasData() {
		if err := refresh(db, client, xmltvURL, pollInterval); err != nil {
			log.Printf("warning: initial fetch failed: %v", err)
		}
	} else {
		db.SetNextRefresh(time.Now().Add(pollInterval))
		log.Printf("skipping initial fetch, data already present (next refresh in %s)", pollInterval)
	}
}

// spaHandler returns an http.Handler that serves static files from fsys,
// falling back to index.html for any path that doesn't match a real file.
// This enables client-side (History API) routing in the SPA.
func spaHandler(fsys http.FileSystem) http.Handler {
	fileServer := http.FileServer(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to open the requested file. If it exists, serve it normally.
		path := r.URL.Path

		// Prevent the browser from caching the service worker script itself.
		// The browser checks for byte-level changes on each SW update check;
		// stale HTTP caching of sw.js defeats the entire versioning scheme.
		if path == "/sw.js" {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		}

		if path == "/" {
			fileServer.ServeHTTP(w, r)
			return
		}
		f, err := fsys.Open(path)
		if err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// File not found — serve index.html for SPA client-side routing.
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

// parseStripWords splits the CHANNEL_NAME_STRIP value on commas and trims whitespace.
func parseStripWords(raw string) []string {
	if raw == "" {
		return nil
	}
	var words []string
	for _, token := range strings.Split(raw, ",") {
		token = strings.TrimSpace(token)
		if token != "" {
			words = append(words, token)
		}
	}
	return words
}

// parseHiddenChannels splits the HIDDEN_CHANNELS value on commas and classifies
// each token: integers are treated as LCN numbers, anything else as a channel ID.
func parseHiddenChannels(raw string) (ids []string, lcns []int) {
	if raw == "" {
		return nil, nil
	}
	for _, token := range strings.Split(raw, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if n, err := strconv.Atoi(token); err == nil {
			lcns = append(lcns, n)
		} else {
			ids = append(ids, token)
		}
	}
	return ids, lcns
}

func refresh(db *database.DB, client *http.Client, url string, interval time.Duration) error {
	log.Printf("fetching XMLTV from %s", url)
	tv, err := xmltv.Fetch(context.Background(), client, url)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	if err := db.Refresh(context.Background(), tv, time.Now().Add(interval)); err != nil {
		return fmt.Errorf("storing data: %w", err)
	}
	log.Printf("loaded %d channels, %d programmes", len(tv.Channels), len(tv.Programmes))
	return nil
}
