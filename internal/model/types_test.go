package model_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/acbgbca/xmltvguide/internal/model"
)

// TestModelTypes verifies that the domain types have the expected fields and
// JSON serialisation tags. This is a compile-time + structural test: if a field
// is missing or renamed, this file will fail to compile.
func TestChannelFields(t *testing.T) {
	lcn := 7
	ch := model.Channel{
		ID:          "ch1",
		DisplayName: "ABC",
		Icon:        "/images/channel/ch1",
		LCN:         &lcn,
	}
	data, err := json.Marshal(ch)
	if err != nil {
		t.Fatalf("marshal Channel: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"id", "displayName", "icon", "lcn"} {
		if _, ok := m[key]; !ok {
			t.Errorf("Channel JSON missing key %q", key)
		}
	}
}

func TestAiringFields(t *testing.T) {
	a := model.Airing{
		ChannelID: "ch1",
		Start:     time.Now(),
		Stop:      time.Now().Add(30 * time.Minute),
		Title:     "Test Show",
	}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal Airing: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"channelId", "start", "stop", "title", "isRepeat", "isPremiere"} {
		if _, ok := m[key]; !ok {
			t.Errorf("Airing JSON missing key %q", key)
		}
	}
}

func TestStatusFields(t *testing.T) {
	s := model.Status{
		LastRefresh: time.Now(),
		NextRefresh: time.Now().Add(time.Hour),
		SourceURL:   "http://example.com/xmltv",
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal Status: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"lastRefresh", "nextRefresh", "sourceUrl"} {
		if _, ok := m[key]; !ok {
			t.Errorf("Status JSON missing key %q", key)
		}
	}
}

func TestSearchResultEmbedding(t *testing.T) {
	// SearchResult must embed Airing and add ChannelName.
	sr := model.SearchResult{
		Airing: model.Airing{
			ChannelID: "ch1",
			Title:     "Test",
		},
		ChannelName: "ABC",
		Rank:        0.5,
	}
	data, err := json.Marshal(sr)
	if err != nil {
		t.Fatalf("marshal SearchResult: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := m["channelName"]; !ok {
		t.Error("SearchResult JSON missing key \"channelName\"")
	}
	// Rank has json:"-" so it must NOT appear in JSON.
	if _, ok := m["rank"]; ok {
		t.Error("SearchResult JSON should not include \"rank\" (json:\"-\")")
	}
}
