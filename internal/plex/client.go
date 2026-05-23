package plex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/acbgbca/xmltvguide/internal/logging"
)

// ErrUnauthorized is returned by all client methods when the server responds
// with 401 or 403. Callers can match it with errors.Is to surface an
// actionable "PLEX_TOKEN appears invalid" message.
var ErrUnauthorized = errors.New("plex: unauthorized")

// Client is a typed HTTP client for the Plex EPG endpoints used by this app.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient builds a Client. baseURL is the Plex Media Server origin (no
// trailing slash required). httpClient, if nil, falls back to a default
// client with a sensible timeout.
func NewClient(baseURL, token string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: httpClient,
	}
}

// Ping issues GET /identity. Returns nil on 200, ErrUnauthorized on 401/403,
// wrapped error otherwise.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.doRequest(ctx, "/identity", nil)
	return err
}

// GetDVRs issues GET /livetv/dvrs.
func (c *Client) GetDVRs(ctx context.Context) ([]DVR, error) {
	body, err := c.doRequest(ctx, "/livetv/dvrs", nil)
	if err != nil {
		return nil, err
	}
	var out dvrsResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("plex GET /livetv/dvrs: json decode: %w", err)
	}
	return out.MediaContainer.Dvr, nil
}

// GetLineupChannels issues GET /epg/lineups/{lineupID}/channels.
func (c *Client) GetLineupChannels(ctx context.Context, lineupID string) ([]LineupChannel, error) {
	path := "/epg/lineups/" + lineupID + "/channels"
	body, err := c.doRequest(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	var out lineupChannelsResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("plex GET %s: json decode: %w", path, err)
	}
	return out.MediaContainer.Channel, nil
}

// GetGrid issues GET /{gridKey}/grid?type=4&beginsAt=<unix>&endsAt=<unix>.
func (c *Client) GetGrid(ctx context.Context, gridKey string, beginsAt, endsAt time.Time) ([]GridEntry, error) {
	// gridKey is a path prefix returned by /livetv/dvrs; preserve its leading
	// slash but tolerate inputs without one.
	if !strings.HasPrefix(gridKey, "/") {
		gridKey = "/" + gridKey
	}
	path := gridKey + "/grid"
	query := map[string]string{
		"type":     "4",
		"beginsAt": strconv.FormatInt(beginsAt.Unix(), 10),
		"endsAt":   strconv.FormatInt(endsAt.Unix(), 10),
	}
	body, err := c.doRequest(ctx, path, query)
	if err != nil {
		return nil, err
	}
	var out gridResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("plex GET %s: json decode: %w", path, err)
	}
	return out.MediaContainer.Metadata, nil
}

// doRequest is the single point at which all requests are issued. It sets the
// auth + accept headers, executes the request, logs a debug line summarising
// the result, and maps 401/403 to ErrUnauthorized.
func (c *Client) doRequest(ctx context.Context, path string, query map[string]string) ([]byte, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("plex GET %s: %w", path, err)
	}
	if len(query) > 0 {
		q := req.URL.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}
	req.Header.Set("X-Plex-Token", c.token)
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("plex GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, readErr := io.ReadAll(resp.Body)
	elapsed := time.Since(start)
	logging.Debug(fmt.Sprintf("plex GET %s → %d (%d bytes, %dms)", path, resp.StatusCode, len(body), elapsed.Milliseconds()))

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("plex GET %s: %w", path, ErrUnauthorized)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("plex GET %s: unexpected status %d", path, resp.StatusCode)
	}
	if readErr != nil {
		return nil, fmt.Errorf("plex GET %s: reading body: %w", path, readErr)
	}
	return body, nil
}
