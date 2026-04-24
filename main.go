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

type config struct {
	xmltvURL       string
	pollInterval   time.Duration
	retentionDays  int
	dbPath         string
	imageCacheDir  string
	port           string
	hiddenIDs      []string
	hiddenLCNs     []int
	stripWords     []string
	rssTTL         int
	refreshOnStart bool
}

func parseConfig() (config, error) {
	xmltvURL := os.Getenv("XMLTV_URL")
	if xmltvURL == "" {
		return config{}, fmt.Errorf("XMLTV_URL environment variable is required")
	}

	pollInterval := 12 * time.Hour
	if v := os.Getenv("POLL_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return config{}, fmt.Errorf("invalid POLL_INTERVAL %q: %w", v, err)
		}
		pollInterval = d
	}

	retentionDays := 7
	if v := os.Getenv("RETENTION_DAYS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return config{}, fmt.Errorf("invalid RETENTION_DAYS %q: must be a positive integer", v)
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

	rssTTL := 0
	if v := os.Getenv("RSS_TTL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			rssTTL = n
		}
	}

	refreshOnStart := os.Getenv("REFRESH_ON_START") == "true"

	return config{
		xmltvURL:       xmltvURL,
		pollInterval:   pollInterval,
		retentionDays:  retentionDays,
		dbPath:         dbPath,
		imageCacheDir:  imageCacheDir,
		port:           port,
		hiddenIDs:      hiddenIDs,
		hiddenLCNs:     hiddenLCNs,
		stripWords:     stripWords,
		rssTTL:         rssTTL,
		refreshOnStart: refreshOnStart,
	}, nil
}

func run(cfg config) error {
	// Ensure the database directory exists (relevant when running outside Docker).
	if err := os.MkdirAll(filepath.Dir(cfg.dbPath), 0750); err != nil {
		return fmt.Errorf("creating database directory %s: %w", filepath.Dir(cfg.dbPath), err)
	}

	// Ensure the image cache directory exists.
	if err := os.MkdirAll(filepath.Join(cfg.imageCacheDir, "channels"), 0750); err != nil {
		return fmt.Errorf("creating image cache directory %s: %w", cfg.imageCacheDir, err)
	}

	httpClient := &http.Client{Timeout: 5 * time.Minute}

	imageCache := images.NewCache(httpClient, cfg.imageCacheDir)

	db, err := database.Open(cfg.dbPath, cfg.retentionDays, cfg.xmltvURL, imageCache, cfg.hiddenIDs, cfg.hiddenLCNs, cfg.stripWords)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}

	runInitialRefresh(db, httpClient, cfg.xmltvURL, cfg.pollInterval, cfg.refreshOnStart)

	// Background refresh goroutine.
	ticker := time.NewTicker(cfg.pollInterval)
	go func() {
		defer ticker.Stop()
		for range ticker.C {
			if err := refresh(db, httpClient, cfg.xmltvURL, cfg.pollInterval); err != nil {
				log.Printf("refresh error: %v", err)
			}
		}
	}()

	mux := http.NewServeMux()

	apiHandler := api.New(db, cfg.rssTTL, func() error {
		return refresh(db, httpClient, cfg.xmltvURL, cfg.pollInterval)
	})
	apiHandler.RegisterRoutes(mux)

	webContent, err := fs.Sub(webFS, "web")
	if err != nil {
		return fmt.Errorf("embedding web files: %w", err)
	}
	mux.Handle("/", spaHandler(http.FS(webContent)))

	srv := &http.Server{
		Addr:              ":" + cfg.port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	log.Printf("TV Guide starting on :%s (poll: %s, retention: %d days, db: %s)", //nolint:gosec // G706: values are from admin-configured env vars, not user input
		cfg.port, cfg.pollInterval, cfg.retentionDays, cfg.dbPath)

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
	return nil
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--healthcheck" {
		if err := healthcheck(); err != nil {
			log.Printf("healthcheck failed: %v", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	cfg, err := parseConfig()
	if err != nil {
		log.Fatal(err)
	}
	if err := run(cfg); err != nil {
		log.Fatal(err)
	}
}

// healthcheck performs an HTTP GET to /api/health and returns nil on success.
func healthcheck() error {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	if _, err := strconv.Atoi(port); err != nil {
		return fmt.Errorf("invalid PORT %q: must be a number", port)
	}
	url := "http://localhost:" + port + "/api/health"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil) //nolint:gosec // G704: host is hardcoded to localhost; port is validated as numeric above
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req) //nolint:gosec // G704: see above
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

func runInitialRefresh(db *database.DB, client *http.Client, xmltvURL string, pollInterval time.Duration, refreshOnStart bool) {
	// Perform initial data fetch only when explicitly requested or when the
	// database is empty (fresh install). Otherwise schedule the first refresh
	// at the normal poll interval so restarts don't hammer the XMLTV source.
	if refreshOnStart || !db.HasData(context.Background()) {
		if err := refresh(db, client, xmltvURL, pollInterval); err != nil {
			log.Printf("warning: initial fetch failed: %v", err)
		}
	} else {
		db.SetNextRefresh(time.Now().Add(pollInterval))
		log.Printf("skipping initial fetch, data already present (next refresh in %s)", pollInterval) //nolint:gosec // G706: value is derived from admin-configured env var, not user input
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
	log.Printf("fetching XMLTV from %s", url) //nolint:gosec // G706: value is from admin-configured env var, not user input
	tv, err := xmltv.Fetch(context.Background(), client, url)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	if err := db.Refresh(context.Background(), tv, time.Now().Add(interval)); err != nil {
		return fmt.Errorf("storing data: %w", err)
	}
	log.Printf("loaded %d channels, %d programmes", len(tv.Channels), len(tv.Programmes)) //nolint:gosec // G706: values are integer counts from parsed XMLTV, not user input
	return nil
}
