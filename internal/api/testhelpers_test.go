package api_test

import (
	"context"
	"io"
	"net/http"
	"testing"
)

// httpGet performs a GET request with a background context.
// It is a context-aware replacement for http.Get in tests.
func httpGet(t *testing.T, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}

// httpPost performs a POST request with a background context and JSON content type.
// It is a context-aware replacement for http.Post in tests.
func httpPost(t *testing.T, url string, body io.Reader) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}
