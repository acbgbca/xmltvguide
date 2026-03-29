package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
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
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestMain(m *testing.M) {
	time.Local = time.UTC
	os.Exit(m.Run())
}

func startWiremock(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "wiremock/wiremock:3",
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

func newIntegrationServer(t *testing.T, xmltvURL string) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"), 7, xmltvURL)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	tv, err := xmltv.Fetch(xmltvURL + "/xmltv")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if err := db.Refresh(tv, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	mux := http.NewServeMux()
	handler := api.New(db)
	handler.RegisterRoutes(mux)

	webSub, err := fs.Sub(webFS, "web")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(webSub)))

	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})
	return srv
}

func TestIntegration_StaticFiles(t *testing.T) {
	baseURL := startWiremock(t)

	xmlBytes, err := os.ReadFile("testdata/sample.xml")
	if err != nil {
		t.Fatalf("read sample.xml: %v", err)
	}
	configureWiremockStub(t, baseURL, string(xmlBytes))

	srv := newIntegrationServer(t, baseURL)

	t.Run("index_html", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/")
		if err != nil {
			t.Fatalf("GET /: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Errorf("Content-Type: expected text/html, got %q", ct)
		}
		var buf strings.Builder
		buf.ReadFrom(resp.Body)
		body := buf.String()
		if !strings.Contains(body, "TV Guide") {
			t.Error("expected body to contain 'TV Guide'")
		}
	})

	t.Run("app_js", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/app.js")
		if err != nil {
			t.Fatalf("GET /app.js: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "javascript") {
			t.Errorf("Content-Type: expected javascript, got %q", ct)
		}
	})

	t.Run("manifest_json", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/manifest.json")
		if err != nil {
			t.Fatalf("GET /manifest.json: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})
}

func TestIntegration_Channels(t *testing.T) {
	baseURL := startWiremock(t)

	xmlBytes, err := os.ReadFile("testdata/sample.xml")
	if err != nil {
		t.Fatalf("read sample.xml: %v", err)
	}
	configureWiremockStub(t, baseURL, string(xmlBytes))

	srv := newIntegrationServer(t, baseURL)

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

func TestIntegration_Guide(t *testing.T) {
	baseURL := startWiremock(t)

	xmlBytes, err := os.ReadFile("testdata/sample.xml")
	if err != nil {
		t.Fatalf("read sample.xml: %v", err)
	}
	configureWiremockStub(t, baseURL, string(xmlBytes))

	srv := newIntegrationServer(t, baseURL)

	resp, err := http.Get(srv.URL + "/api/guide?date=2026-03-29")
	if err != nil {
		t.Fatalf("GET /api/guide: %v", err)
	}
	defer resp.Body.Close()

	var result []struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 4 {
		t.Fatalf("expected 4 airings, got %d", len(result))
	}
}

func TestIntegration_Status(t *testing.T) {
	baseURL := startWiremock(t)

	xmlBytes, err := os.ReadFile("testdata/sample.xml")
	if err != nil {
		t.Fatalf("read sample.xml: %v", err)
	}
	configureWiremockStub(t, baseURL, string(xmlBytes))

	srv := newIntegrationServer(t, baseURL)

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
	if !strings.Contains(result.SourceUrl, baseURL) {
		t.Errorf("sourceUrl: expected to contain %q, got %q", baseURL, result.SourceUrl)
	}
}
