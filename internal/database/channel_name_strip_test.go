package database_test

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/acbgbca/xmltvguide/internal/database"
	"github.com/acbgbca/xmltvguide/internal/images"
	"github.com/acbgbca/xmltvguide/internal/xmltv"
)

// openTestDBWithStripWords opens a fresh database configured with the given strip words.
func openTestDBWithStripWords(t *testing.T, stripWords []string) *database.DB {
	t.Helper()
	dir := t.TempDir()
	client := &http.Client{Transport: &failingTransport{}}
	cache := images.NewCache(client, filepath.Join(dir, "images"))
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test", cache, nil, nil, stripWords)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// stripSampleTV returns a TV with channels that have geographic suffixes to strip.
func stripSampleTV() *xmltv.TV {
	base := testBaseDate()
	return &xmltv.TV{
		Channels: []xmltv.Channel{
			{
				ID:           "ch1",
				DisplayNames: []xmltv.Name{{Value: "ABC Melbourne"}},
				LCN:          "2",
			},
			{
				ID:           "ch2",
				DisplayNames: []xmltv.Name{{Value: "Nine Victoria"}},
			},
			{
				ID:           "ch3",
				DisplayNames: []xmltv.Name{{Value: "Seven"}},
			},
		},
		Programmes: []xmltv.Programme{
			{
				Start:   xmltv.XmltvTime{Time: base.Add(6 * time.Hour)},
				Stop:    xmltv.XmltvTime{Time: base.Add(7 * time.Hour)},
				Channel: "ch1",
				Titles:  []xmltv.Name{{Value: "Morning News"}},
			},
		},
	}
}

// TestRefresh_ChannelNameStrip verifies that a strip word is removed from display names.
func TestRefresh_ChannelNameStrip(t *testing.T) {
	db := openTestDBWithStripWords(t, []string{"Melbourne"})
	tv := stripSampleTV()
	if err := db.Refresh(context.Background(), tv, testBaseDate()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	channels, err := db.GetChannels()
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}

	for _, ch := range channels {
		if ch.ID == "ch1" {
			if ch.DisplayName != "ABC" {
				t.Errorf("expected name %q, got %q", "ABC", ch.DisplayName)
			}
		}
		if ch.ID == "ch2" {
			if ch.DisplayName != "Nine Victoria" {
				t.Errorf("expected name %q (unaffected), got %q", "Nine Victoria", ch.DisplayName)
			}
		}
	}
}

// TestRefresh_ChannelNameStrip_CaseInsensitive verifies that strip words match regardless of case.
func TestRefresh_ChannelNameStrip_CaseInsensitive(t *testing.T) {
	db := openTestDBWithStripWords(t, []string{"melbourne"})
	tv := stripSampleTV()
	if err := db.Refresh(context.Background(), tv, testBaseDate()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	channels, err := db.GetChannels()
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}

	for _, ch := range channels {
		if ch.ID == "ch1" {
			if ch.DisplayName != "ABC" {
				t.Errorf("expected name %q, got %q (case-insensitive strip failed)", "ABC", ch.DisplayName)
			}
		}
	}
}

// TestRefresh_ChannelNameStrip_TrimSpace verifies that the result is trimmed of surrounding whitespace.
func TestRefresh_ChannelNameStrip_TrimSpace(t *testing.T) {
	db := openTestDBWithStripWords(t, []string{"Victoria"})
	tv := stripSampleTV()
	if err := db.Refresh(context.Background(), tv, testBaseDate()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	channels, err := db.GetChannels()
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}

	for _, ch := range channels {
		if ch.ID == "ch2" {
			if ch.DisplayName != "Nine" {
				t.Errorf("expected trimmed name %q, got %q", "Nine", ch.DisplayName)
			}
		}
	}
}

// TestRefresh_ChannelNameStrip_Empty verifies that empty CHANNEL_NAME_STRIP leaves names unchanged.
func TestRefresh_ChannelNameStrip_Empty(t *testing.T) {
	db := openTestDBWithStripWords(t, nil)
	tv := stripSampleTV()
	if err := db.Refresh(context.Background(), tv, testBaseDate()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	channels, err := db.GetChannels()
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}

	for _, ch := range channels {
		switch ch.ID {
		case "ch1":
			if ch.DisplayName != "ABC Melbourne" {
				t.Errorf("expected unchanged name %q, got %q", "ABC Melbourne", ch.DisplayName)
			}
		case "ch2":
			if ch.DisplayName != "Nine Victoria" {
				t.Errorf("expected unchanged name %q, got %q", "Nine Victoria", ch.DisplayName)
			}
		}
	}
}
