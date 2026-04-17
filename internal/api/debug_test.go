package api_test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestPostDebugLog(t *testing.T) {
	srv := newSeededServer(t)

	t.Run("valid JSON body returns 204", func(t *testing.T) {
		body := `{"type":"onerror","message":"something went wrong","source":"app.js","lineno":42,"colno":7,"stack":"Error: something\n    at app.js:42","url":"/guide"}`
		resp, err := httpPost(t, srv.URL+"/api/debug/log", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Errorf("status = %d, want 204", resp.StatusCode)
		}
	})

	t.Run("empty body returns 204", func(t *testing.T) {
		resp, err := httpPost(t, srv.URL+"/api/debug/log", bytes.NewReader([]byte("{}")))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Errorf("status = %d, want 204", resp.StatusCode)
		}
	})

	t.Run("malformed JSON returns 400", func(t *testing.T) {
		resp, err := httpPost(t, srv.URL+"/api/debug/log", strings.NewReader("not-json"))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("oversized body returns 400", func(t *testing.T) {
		large := bytes.Repeat([]byte("x"), 65*1024)
		body := append([]byte(`{"message":"`), append(large, []byte(`"}`)...)...)
		resp, err := httpPost(t, srv.URL+"/api/debug/log", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})
}
