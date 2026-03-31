package xmltv_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/acbgbca/xmltvguide/internal/xmltv"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func startWiremock(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "wiremock/wiremock:latest",
			ExposedPorts: []string{"8080/tcp"},
			WaitingFor:   wait.ForHTTP("/__admin/mappings").WithPort("8080/tcp"),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start wiremock: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if err := container.Terminate(cleanupCtx); err != nil {
			t.Logf("terminate wiremock: %v", err)
		}
	})
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("wiremock host: %v", err)
	}
	port, err := container.MappedPort(ctx, "8080/tcp")
	if err != nil {
		t.Fatalf("wiremock port: %v", err)
	}
	return fmt.Sprintf("http://%s:%s", host, port.Port())
}

func configureWiremockStub(t *testing.T, baseURL, xmlContent string) {
	t.Helper()
	escapedBody, err := json.Marshal(xmlContent)
	if err != nil {
		t.Fatalf("marshal xml content: %v", err)
	}
	stubJSON := fmt.Sprintf(`{
		"request": {
			"method": "GET",
			"url": "/xmltv"
		},
		"response": {
			"status": 200,
			"body": %s,
			"headers": {
				"Content-Type": "text/xml"
			}
		}
	}`, string(escapedBody))

	resp, err := http.Post(baseURL+"/__admin/mappings", "application/json", strings.NewReader(stubJSON))
	if err != nil {
		t.Fatalf("configure wiremock stub: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("configure wiremock stub: unexpected status %d", resp.StatusCode)
	}
}

const minimalXML = `<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <channel id="ch1">
    <display-name>ABC</display-name>
    <lcn>2</lcn>
  </channel>
  <channel id="ch2">
    <display-name>SBS</display-name>
  </channel>
  <programme start="20260329060000 +0000" stop="20260329070000 +0000" channel="ch1">
    <title>Morning News</title>
  </programme>
</tv>`

func TestParse_ChannelLCN(t *testing.T) {
	tv, err := xmltv.Parse(strings.NewReader(minimalXML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(tv.Channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(tv.Channels))
	}
	var ch1, ch2 *xmltv.Channel
	for i := range tv.Channels {
		if tv.Channels[i].ID == "ch1" {
			ch1 = &tv.Channels[i]
		}
		if tv.Channels[i].ID == "ch2" {
			ch2 = &tv.Channels[i]
		}
	}
	if ch1 == nil || ch1.LCN != "2" {
		t.Errorf("ch1 LCN: expected %q, got %q", "2", ch1.LCN)
	}
	if ch2 == nil || ch2.LCN != "" {
		t.Errorf("ch2 LCN: expected empty, got %q", ch2.LCN)
	}
}

func TestParse_Channels(t *testing.T) {
	tv, err := xmltv.Parse(strings.NewReader(minimalXML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(tv.Channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(tv.Channels))
	}
	ids := map[string]bool{}
	for _, ch := range tv.Channels {
		ids[ch.ID] = true
	}
	if !ids["ch1"] {
		t.Error("expected channel ch1")
	}
	if !ids["ch2"] {
		t.Error("expected channel ch2")
	}
}

const fullProgrammeXML = `<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <channel id="ch1">
    <display-name>ABC</display-name>
  </channel>
  <programme start="20260329070000 +0000" stop="20260329090000 +0000" channel="ch1">
    <title>Sunrise</title>
    <sub-title>Monday Edition</sub-title>
    <desc>Morning breakfast television.</desc>
    <category>Entertainment</category>
    <episode-num system="xmltv_ns">5.12.0/1</episode-num>
    <episode-num system="onscreen">S06 E13</episode-num>
    <star-rating system="imdb">
      <value>3.5/5</value>
    </star-rating>
    <previously-shown/>
    <premiere>First run</premiere>
    <date>2026</date>
  </programme>
</tv>`

func TestParse_ProgrammeFields(t *testing.T) {
	tv, err := xmltv.Parse(strings.NewReader(fullProgrammeXML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(tv.Programmes) != 1 {
		t.Fatalf("expected 1 programme, got %d", len(tv.Programmes))
	}
	p := tv.Programmes[0]

	if len(p.Titles) == 0 || p.Titles[0].Value != "Sunrise" {
		t.Errorf("Title: expected %q", "Sunrise")
	}
	if len(p.SubTitles) == 0 || p.SubTitles[0].Value != "Monday Edition" {
		t.Errorf("SubTitle: expected %q", "Monday Edition")
	}
	if len(p.Categories) == 0 || p.Categories[0].Value != "Entertainment" {
		t.Errorf("Category: expected %q", "Entertainment")
	}
	if p.Date != "2026" {
		t.Errorf("Date: expected %q, got %q", "2026", p.Date)
	}
	if p.PreviouslyShown == nil {
		t.Error("expected PreviouslyShown to be set")
	}
	if p.Premiere == nil || p.Premiere.Value != "First run" {
		t.Error("expected Premiere to be set with value 'First run'")
	}

	var xmltvNS, onscreen string
	for _, ep := range p.EpisodeNums {
		if ep.System == "xmltv_ns" {
			xmltvNS = ep.Value
		}
		if ep.System == "onscreen" {
			onscreen = ep.Value
		}
	}
	if xmltvNS != "5.12.0/1" {
		t.Errorf("EpisodeNum xmltv_ns: expected %q, got %q", "5.12.0/1", xmltvNS)
	}
	if onscreen != "S06 E13" {
		t.Errorf("EpisodeNum onscreen: expected %q, got %q", "S06 E13", onscreen)
	}

	if len(p.StarRatings) == 0 || p.StarRatings[0].Value != "3.5/5" {
		t.Errorf("StarRating: expected %q", "3.5/5")
	}

	expectedStart := time.Date(2026, 3, 29, 7, 0, 0, 0, time.UTC)
	if !p.Start.Equal(expectedStart) {
		t.Errorf("Start: expected %v, got %v", expectedStart, p.Start)
	}
}

const xmlWithColonTZ = `<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <channel id="ch1">
    <display-name>ABC</display-name>
  </channel>
  <programme start="20260329060000 +00:00" stop="20260329070000 +00:00" channel="ch1">
    <title>Morning News</title>
  </programme>
</tv>`

func TestParse_TimeFormats(t *testing.T) {
	t.Run("plus_offset_no_colon", func(t *testing.T) {
		tv, err := xmltv.Parse(strings.NewReader(minimalXML))
		if err != nil {
			t.Fatalf("Parse with +0000 format: %v", err)
		}
		if len(tv.Programmes) != 1 {
			t.Fatalf("expected 1 programme, got %d", len(tv.Programmes))
		}
	})

	t.Run("plus_offset_with_colon", func(t *testing.T) {
		tv, err := xmltv.Parse(strings.NewReader(xmlWithColonTZ))
		if err != nil {
			t.Fatalf("Parse with +00:00 format: %v", err)
		}
		if len(tv.Programmes) != 1 {
			t.Fatalf("expected 1 programme, got %d", len(tv.Programmes))
		}
		p := tv.Programmes[0]
		expected := time.Date(2026, 3, 29, 6, 0, 0, 0, time.UTC)
		if !p.Start.Equal(expected) {
			t.Errorf("Start time: expected %v, got %v", expected, p.Start)
		}
	})
}

func TestFetch_ViaWiremock(t *testing.T) {
	baseURL := startWiremock(t)

	xmlBytes, err := os.ReadFile("../../testdata/sample.xml")
	if err != nil {
		t.Fatalf("read sample.xml: %v", err)
	}
	configureWiremockStub(t, baseURL, string(xmlBytes))

	tv, err := xmltv.Fetch(baseURL + "/xmltv")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(tv.Channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(tv.Channels))
	}
	if len(tv.Programmes) != 4 {
		t.Fatalf("expected 4 programmes, got %d", len(tv.Programmes))
	}
	if tv.Channels[0].ID != "ch1" {
		t.Errorf("first channel ID: expected %q, got %q", "ch1", tv.Channels[0].ID)
	}
	if len(tv.Programmes[0].Titles) == 0 || tv.Programmes[0].Titles[0].Value != "Late Night Movie" {
		t.Errorf("first programme title: expected %q", "Late Night Movie")
	}
}
