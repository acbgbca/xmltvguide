package main

import (
	"testing"
	"time"
)

func TestParseConfig_Defaults(t *testing.T) {
	// Set only the required env var.
	t.Setenv("XMLTV_URL", "http://example.com/guide.xml")
	// Clear optional vars so defaults kick in.
	t.Setenv("POLL_INTERVAL", "")
	t.Setenv("RETENTION_DAYS", "")
	t.Setenv("DB_PATH", "")
	t.Setenv("IMAGE_CACHE_DIR", "")
	t.Setenv("PORT", "")
	t.Setenv("HIDDEN_CHANNELS", "")
	t.Setenv("CHANNEL_NAME_STRIP", "")
	t.Setenv("RSS_TTL", "")
	t.Setenv("REFRESH_ON_START", "")

	cfg, err := parseConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.xmltvURL != "http://example.com/guide.xml" {
		t.Errorf("xmltvURL = %q, want %q", cfg.xmltvURL, "http://example.com/guide.xml")
	}
	if cfg.pollInterval != 12*time.Hour {
		t.Errorf("pollInterval = %v, want %v", cfg.pollInterval, 12*time.Hour)
	}
	if cfg.retentionDays != 7 {
		t.Errorf("retentionDays = %d, want %d", cfg.retentionDays, 7)
	}
	if cfg.dbPath != "/data/tvguide.db" {
		t.Errorf("dbPath = %q, want %q", cfg.dbPath, "/data/tvguide.db")
	}
	if cfg.imageCacheDir != "/data/images" {
		t.Errorf("imageCacheDir = %q, want %q", cfg.imageCacheDir, "/data/images")
	}
	if cfg.port != "8080" {
		t.Errorf("port = %q, want %q", cfg.port, "8080")
	}
	if len(cfg.hiddenIDs) != 0 {
		t.Errorf("hiddenIDs = %v, want empty", cfg.hiddenIDs)
	}
	if len(cfg.hiddenLCNs) != 0 {
		t.Errorf("hiddenLCNs = %v, want empty", cfg.hiddenLCNs)
	}
	if len(cfg.stripWords) != 0 {
		t.Errorf("stripWords = %v, want empty", cfg.stripWords)
	}
	if cfg.rssTTL != 0 {
		t.Errorf("rssTTL = %d, want %d", cfg.rssTTL, 0)
	}
	if cfg.refreshOnStart {
		t.Errorf("refreshOnStart = true, want false")
	}
}

func TestParseConfig_MissingXMLTVURL(t *testing.T) {
	t.Setenv("XMLTV_URL", "")

	_, err := parseConfig()
	if err == nil {
		t.Fatal("expected error when XMLTV_URL is missing, got nil")
	}
}

func TestParseConfig_CustomValues(t *testing.T) {
	t.Setenv("XMLTV_URL", "http://example.com/guide.xml")
	t.Setenv("POLL_INTERVAL", "30m")
	t.Setenv("RETENTION_DAYS", "14")
	t.Setenv("DB_PATH", "/tmp/test.db")
	t.Setenv("IMAGE_CACHE_DIR", "/tmp/images")
	t.Setenv("PORT", "9090")
	t.Setenv("HIDDEN_CHANNELS", "ch1,7,9")
	t.Setenv("CHANNEL_NAME_STRIP", "HD, FHD")
	t.Setenv("RSS_TTL", "120")
	t.Setenv("REFRESH_ON_START", "true")

	cfg, err := parseConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.pollInterval != 30*time.Minute {
		t.Errorf("pollInterval = %v, want %v", cfg.pollInterval, 30*time.Minute)
	}
	if cfg.retentionDays != 14 {
		t.Errorf("retentionDays = %d, want %d", cfg.retentionDays, 14)
	}
	if cfg.dbPath != "/tmp/test.db" {
		t.Errorf("dbPath = %q, want %q", cfg.dbPath, "/tmp/test.db")
	}
	if cfg.imageCacheDir != "/tmp/images" {
		t.Errorf("imageCacheDir = %q, want %q", cfg.imageCacheDir, "/tmp/images")
	}
	if cfg.port != "9090" {
		t.Errorf("port = %q, want %q", cfg.port, "9090")
	}
	// hiddenIDs: "ch1"; hiddenLCNs: 7, 9
	if len(cfg.hiddenIDs) != 1 || cfg.hiddenIDs[0] != "ch1" {
		t.Errorf("hiddenIDs = %v, want [ch1]", cfg.hiddenIDs)
	}
	if len(cfg.hiddenLCNs) != 2 || cfg.hiddenLCNs[0] != 7 || cfg.hiddenLCNs[1] != 9 {
		t.Errorf("hiddenLCNs = %v, want [7 9]", cfg.hiddenLCNs)
	}
	if len(cfg.stripWords) != 2 || cfg.stripWords[0] != "HD" || cfg.stripWords[1] != "FHD" {
		t.Errorf("stripWords = %v, want [HD FHD]", cfg.stripWords)
	}
	if cfg.rssTTL != 120 {
		t.Errorf("rssTTL = %d, want %d", cfg.rssTTL, 120)
	}
	if !cfg.refreshOnStart {
		t.Errorf("refreshOnStart = false, want true")
	}
}

func TestParseConfig_InvalidPollInterval(t *testing.T) {
	t.Setenv("XMLTV_URL", "http://example.com/guide.xml")
	t.Setenv("POLL_INTERVAL", "notaduration")

	_, err := parseConfig()
	if err == nil {
		t.Fatal("expected error for invalid POLL_INTERVAL, got nil")
	}
}

func TestParseConfig_InvalidRetentionDays(t *testing.T) {
	t.Setenv("XMLTV_URL", "http://example.com/guide.xml")
	t.Setenv("POLL_INTERVAL", "")
	t.Setenv("RETENTION_DAYS", "notanumber")

	_, err := parseConfig()
	if err == nil {
		t.Fatal("expected error for invalid RETENTION_DAYS, got nil")
	}
}

func TestParseConfig_ZeroRetentionDays(t *testing.T) {
	t.Setenv("XMLTV_URL", "http://example.com/guide.xml")
	t.Setenv("POLL_INTERVAL", "")
	t.Setenv("RETENTION_DAYS", "0")

	_, err := parseConfig()
	if err == nil {
		t.Fatal("expected error for RETENTION_DAYS=0, got nil")
	}
}

func TestParseConfig_InvalidRssTTLIsIgnored(t *testing.T) {
	t.Setenv("XMLTV_URL", "http://example.com/guide.xml")
	t.Setenv("POLL_INTERVAL", "")
	t.Setenv("RETENTION_DAYS", "")
	t.Setenv("RSS_TTL", "notanumber")

	cfg, err := parseConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.rssTTL != 0 {
		t.Errorf("rssTTL = %d, want 0 (invalid value should be ignored)", cfg.rssTTL)
	}
}

func TestParseConfig_NegativeRssTTLIsIgnored(t *testing.T) {
	t.Setenv("XMLTV_URL", "http://example.com/guide.xml")
	t.Setenv("POLL_INTERVAL", "")
	t.Setenv("RETENTION_DAYS", "")
	t.Setenv("RSS_TTL", "-5")

	t.Setenv("DB_PATH", "")
	t.Setenv("IMAGE_CACHE_DIR", "")
	t.Setenv("PORT", "")
	t.Setenv("HIDDEN_CHANNELS", "")
	t.Setenv("CHANNEL_NAME_STRIP", "")
	t.Setenv("REFRESH_ON_START", "")

	cfg, err := parseConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.rssTTL != 0 {
		t.Errorf("rssTTL = %d, want 0 (negative value should be ignored)", cfg.rssTTL)
	}
}
