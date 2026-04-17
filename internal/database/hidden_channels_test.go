package database_test

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/acbgbca/xmltvguide/internal/database"
	"github.com/acbgbca/xmltvguide/internal/images"
)

// openTestDBHidden opens a fresh database with hidden channel configuration.
func openTestDBHidden(t *testing.T, hiddenIDs []string, hiddenLCNs []int) *database.DB {
	t.Helper()
	dir := t.TempDir()
	client := &http.Client{Transport: &failingTransport{}}
	cache := images.NewCache(client, filepath.Join(dir, "images"))
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test", cache, hiddenIDs, hiddenLCNs, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// refreshHiddenTestData populates the database with the standard sampleTV data.
func refreshHiddenTestData(t *testing.T, db *database.DB) {
	t.Helper()
	tv := sampleTV()
	if err := db.Refresh(context.Background(), tv, testBaseDate()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
}

// TestHiddenChannels_Empty verifies that no channels are filtered when
// HIDDEN_CHANNELS is unset or empty.
func TestHiddenChannels_Empty(t *testing.T) {
	db := openTestDBHidden(t, nil, nil)
	refreshHiddenTestData(t, db)

	channels, err := db.GetChannels(context.Background())
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}
	if len(channels) != 2 {
		t.Errorf("expected 2 channels, got %d", len(channels))
	}
}

// TestHiddenChannels_ByID_GetChannels verifies that a channel excluded by its
// string ID does not appear in GetChannels.
func TestHiddenChannels_ByID_GetChannels(t *testing.T) {
	db := openTestDBHidden(t, []string{"ch1"}, nil)
	refreshHiddenTestData(t, db)

	channels, err := db.GetChannels(context.Background())
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(channels))
	}
	if channels[0].ID == "ch1" {
		t.Error("hidden channel ch1 should not appear in GetChannels")
	}
}

// TestHiddenChannels_ByID_GetAirings verifies that airings for a channel
// excluded by its string ID do not appear in GetAirings.
func TestHiddenChannels_ByID_GetAirings(t *testing.T) {
	db := openTestDBHidden(t, []string{"ch1"}, nil)
	refreshHiddenTestData(t, db)

	airings, err := db.GetAirings(context.Background(), testBaseDate())
	if err != nil {
		t.Fatalf("GetAirings: %v", err)
	}
	for _, a := range airings {
		if a.ChannelID == "ch1" {
			t.Errorf("airings for hidden channel ch1 should not appear in GetAirings, got airing: %q", a.Title)
		}
	}
}

// TestHiddenChannels_ByLCN_GetChannels verifies that a channel excluded by its
// integer LCN does not appear in GetChannels.
// In sampleTV, ch1 has LCN "2".
func TestHiddenChannels_ByLCN_GetChannels(t *testing.T) {
	db := openTestDBHidden(t, nil, []int{2})
	refreshHiddenTestData(t, db)

	channels, err := db.GetChannels(context.Background())
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel after LCN filter, got %d", len(channels))
	}
	if channels[0].ID == "ch1" {
		t.Error("channel with LCN 2 (ch1) should not appear in GetChannels")
	}
}

// TestHiddenChannels_ByLCN_GetAirings verifies that airings for a channel
// excluded by its LCN do not appear in GetAirings.
func TestHiddenChannels_ByLCN_GetAirings(t *testing.T) {
	db := openTestDBHidden(t, nil, []int{2})
	refreshHiddenTestData(t, db)

	airings, err := db.GetAirings(context.Background(), testBaseDate())
	if err != nil {
		t.Fatalf("GetAirings: %v", err)
	}
	for _, a := range airings {
		if a.ChannelID == "ch1" {
			t.Errorf("airings for channel with LCN 2 (ch1) should not appear, got airing: %q", a.Title)
		}
	}
}

// TestHiddenChannels_Mixed verifies that both ID and LCN entries in the same
// hidden list work correctly.
// We hide ch2 by ID and ch1 by LCN — both should be absent.
func TestHiddenChannels_Mixed(t *testing.T) {
	db := openTestDBHidden(t, []string{"ch2"}, []int{2})
	refreshHiddenTestData(t, db)

	channels, err := db.GetChannels(context.Background())
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}
	if len(channels) != 0 {
		t.Errorf("expected 0 channels with both ch1 (LCN 2) and ch2 (ID) hidden, got %d", len(channels))
	}

	airings, err := db.GetAirings(context.Background(), testBaseDate())
	if err != nil {
		t.Fatalf("GetAirings: %v", err)
	}
	if len(airings) != 0 {
		t.Errorf("expected 0 airings with all channels hidden, got %d", len(airings))
	}
}

// TestHiddenChannels_ByID_GetNowNext verifies that hidden channels don't
// appear in GetNowNext results.
func TestHiddenChannels_ByID_GetNowNext(t *testing.T) {
	db := openTestDBHidden(t, []string{"ch1"}, nil)
	refreshHiddenTestData(t, db)

	entries, err := db.GetNowNext(context.Background())
	if err != nil {
		t.Fatalf("GetNowNext: %v", err)
	}
	for _, e := range entries {
		if e.ChannelID == "ch1" {
			t.Error("hidden channel ch1 should not appear in GetNowNext")
		}
	}
}

// TestHiddenChannels_ByID_Search verifies that hidden channels don't appear
// in search results (simple, advanced, and browse).
func TestHiddenChannels_ByID_Search(t *testing.T) {
	db := openTestDBHidden(t, []string{"ch1"}, nil)
	refreshHiddenTestData(t, db)

	t.Run("SearchSimple", func(t *testing.T) {
		results, err := db.SearchSimple(context.Background(), "Morning News", true, false)
		if err != nil {
			t.Fatalf("SearchSimple: %v", err)
		}
		for _, r := range results {
			if r.ChannelID == "ch1" {
				t.Error("hidden channel ch1 should not appear in SearchSimple results")
			}
		}
	})

	t.Run("SearchAdvanced", func(t *testing.T) {
		results, err := db.SearchAdvanced(context.Background(), "Morning", nil, true, true, false)
		if err != nil {
			t.Fatalf("SearchAdvanced: %v", err)
		}
		for _, r := range results {
			if r.ChannelID == "ch1" {
				t.Error("hidden channel ch1 should not appear in SearchAdvanced results")
			}
		}
	})

	t.Run("SearchBrowse", func(t *testing.T) {
		results, err := db.SearchBrowse(context.Background(), nil, false, true, true, false)
		if err != nil {
			t.Fatalf("SearchBrowse: %v", err)
		}
		for _, r := range results {
			if r.ChannelID == "ch1" {
				t.Error("hidden channel ch1 should not appear in SearchBrowse results")
			}
		}
	})
}
