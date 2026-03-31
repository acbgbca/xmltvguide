package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
	_ "time/tzdata" // embed IANA timezone database so the binary works on scratch

	"github.com/acbgbca/xmltvguide/internal/api"
	"github.com/acbgbca/xmltvguide/internal/database"
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

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Ensure the database directory exists (relevant when running outside Docker).
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		log.Fatalf("creating database directory %s: %v", filepath.Dir(dbPath), err)
	}

	db, err := database.Open(dbPath, retentionDays, xmltvURL)
	if err != nil {
		log.Fatalf("opening database: %v", err)
	}
	defer db.Close()

	refreshOnStart := os.Getenv("REFRESH_ON_START") == "true"
	runInitialRefresh(db, xmltvURL, pollInterval, refreshOnStart)

	// Background refresh goroutine.
	ticker := time.NewTicker(pollInterval)
	go func() {
		defer ticker.Stop()
		for range ticker.C {
			if err := refresh(db, xmltvURL, pollInterval); err != nil {
				log.Printf("refresh error: %v", err)
			}
		}
	}()

	mux := http.NewServeMux()

	apiHandler := api.New(db)
	apiHandler.RegisterRoutes(mux)

	webContent, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("embedding web files: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(webContent)))

	log.Printf("TV Guide starting on :%s (poll: %s, retention: %d days, db: %s)",
		port, pollInterval, retentionDays, dbPath)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

func runInitialRefresh(db *database.DB, xmltvURL string, pollInterval time.Duration, refreshOnStart bool) {
	// Perform initial data fetch only when explicitly requested or when the
	// database is empty (fresh install). Otherwise schedule the first refresh
	// at the normal poll interval so restarts don't hammer the XMLTV source.
	if refreshOnStart || !db.HasData() {
		if err := refresh(db, xmltvURL, pollInterval); err != nil {
			log.Printf("warning: initial fetch failed: %v", err)
		}
	} else {
		db.SetNextRefresh(time.Now().Add(pollInterval))
		log.Printf("skipping initial fetch, data already present (next refresh in %s)", pollInterval)
	}
}

func refresh(db *database.DB, url string, interval time.Duration) error {
	log.Printf("fetching XMLTV from %s", url)
	tv, err := xmltv.Fetch(url)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	if err := db.Refresh(tv, time.Now().Add(interval)); err != nil {
		return fmt.Errorf("storing data: %w", err)
	}
	log.Printf("loaded %d channels, %d programmes", len(tv.Channels), len(tv.Programmes))
	return nil
}
