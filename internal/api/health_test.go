package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestGetHealth(t *testing.T) {
	srv := newSeededServer(t)

	t.Run("returns 200 with ok status", func(t *testing.T) {
		resp, err := httpGet(t, srv.URL+"/api/health")
		if err != nil {
			t.Fatalf("GET /api/health: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}

		var body map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["status"] != "ok" {
			t.Errorf("status = %q, want %q", body["status"], "ok")
		}
	})
}
