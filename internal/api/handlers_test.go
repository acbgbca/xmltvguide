package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acbgbca/xmltvguide/internal/api"
	"github.com/acbgbca/xmltvguide/internal/database"
	"github.com/acbgbca/xmltvguide/internal/xmltv"
)

func TestMain(m *testing.M) {
	time.Local = time.UTC
	os.Exit(m.Run())
}

func newSeededServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, "http://test-source")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	tv := &xmltv.TV{
		Channels: []xmltv.Channel{
			{
				ID:           "ch1",
				DisplayNames: []xmltv.Name{{Value: "ABC"}},
				LCN:          "2",
			},
			{
				ID:           "ch2",
				DisplayNames: []xmltv.Name{{Value: "SBS"}},
			},
		},
		Programmes: []xmltv.Programme{
			{
				Start:   xmltv.XmltvTime{Time: time.Date(2026, 3, 29, 6, 0, 0, 0, time.UTC)},
				Stop:    xmltv.XmltvTime{Time: time.Date(2026, 3, 29, 7, 0, 0, 0, time.UTC)},
				Channel: "ch1",
				Titles:  []xmltv.Name{{Value: "Morning News"}},
			},
			{
				Start:      xmltv.XmltvTime{Time: time.Date(2026, 3, 29, 7, 0, 0, 0, time.UTC)},
				Stop:       xmltv.XmltvTime{Time: time.Date(2026, 3, 29, 8, 0, 0, 0, time.UTC)},
				Channel:    "ch2",
				Titles:     []xmltv.Name{{Value: "World News"}},
				Categories: []xmltv.Name{{Value: "News"}},
			},
		},
	}

	if err := db.Refresh(tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	mux := http.NewServeMux()
	handler := api.New(db)
	handler.RegisterRoutes(mux)

	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})
	return srv
}

func TestGetChannels_Count(t *testing.T) {
	srv := newSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/channels")
	if err != nil {
		t.Fatalf("GET /api/channels: %v", err)
	}
	defer resp.Body.Close()

	var result []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(result))
	}
}

func TestGetChannels_Order(t *testing.T) {
	srv := newSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/channels")
	if err != nil {
		t.Fatalf("GET /api/channels: %v", err)
	}
	defer resp.Body.Close()

	var result []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("expected channels, got none")
	}
	if result[0].ID != "ch1" {
		t.Errorf("expected first channel ID %q, got %q", "ch1", result[0].ID)
	}
}

func TestGetChannels_LCN(t *testing.T) {
	srv := newSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/channels")
	if err != nil {
		t.Fatalf("GET /api/channels: %v", err)
	}
	defer resp.Body.Close()

	var result []struct {
		ID  string `json:"id"`
		LCN *int   `json:"lcn"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) < 2 {
		t.Fatalf("expected at least 2 channels, got %d", len(result))
	}
	// ch1 was seeded with LCN=2
	var ch1, ch2 *struct {
		ID  string `json:"id"`
		LCN *int   `json:"lcn"`
	}
	for i := range result {
		if result[i].ID == "ch1" {
			ch1 = &result[i]
		}
		if result[i].ID == "ch2" {
			ch2 = &result[i]
		}
	}
	if ch1 == nil || ch1.LCN == nil || *ch1.LCN != 2 {
		t.Errorf("ch1 LCN: expected 2, got %v", ch1)
	}
	if ch2 == nil || ch2.LCN != nil {
		t.Errorf("ch2 LCN: expected nil, got %v", ch2.LCN)
	}
}

func TestGetChannels_ContentType(t *testing.T) {
	srv := newSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/channels")
	if err != nil {
		t.Fatalf("GET /api/channels: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: expected application/json, got %q", ct)
	}
}

func TestGetGuide_Date(t *testing.T) {
	srv := newSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/guide?date=2026-03-29")
	if err != nil {
		t.Fatalf("GET /api/guide: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result []struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 airings, got %d", len(result))
	}
}

func TestGetGuide_NoDate(t *testing.T) {
	srv := newSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/guide")
	if err != nil {
		t.Fatalf("GET /api/guide: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestGetGuide_InvalidDate(t *testing.T) {
	srv := newSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/guide?date=notadate")
	if err != nil {
		t.Fatalf("GET /api/guide: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGetStatus_SourceURL(t *testing.T) {
	srv := newSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/status")
	if err != nil {
		t.Fatalf("GET /api/status: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		SourceUrl string `json:"sourceUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.SourceUrl != "http://test-source" {
		t.Errorf("sourceUrl: expected %q, got %q", "http://test-source", result.SourceUrl)
	}
}

func TestGetStatus_ContentType(t *testing.T) {
	srv := newSeededServer(t)
	resp, err := http.Get(srv.URL + "/api/status")
	if err != nil {
		t.Fatalf("GET /api/status: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: expected application/json, got %q", ct)
	}
}
