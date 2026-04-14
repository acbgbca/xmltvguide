package images

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Cache manages the local icon file cache and handles downloading icons from
// remote URLs. It is safe to call from multiple goroutines.
type Cache struct {
	httpClient *http.Client
	dir        string
}

// NewCache creates a new Cache that stores files under dir and uses httpClient
// for downloads. Pass nil for httpClient to disable downloading.
func NewCache(httpClient *http.Client, dir string) *Cache {
	return &Cache{httpClient: httpClient, dir: dir}
}

// EnsureIcon returns a valid local path for the channel's icon. If localPath
// exists on disk it is returned as-is. Otherwise the icon is downloaded from
// iconURL and the new local path is returned. Returns ("", nil) when iconURL
// is empty.
func (c *Cache) EnsureIcon(ctx context.Context, channelID, localPath, iconURL string) (string, error) {
	if iconURL == "" {
		return "", nil
	}
	if localPath != "" {
		if _, err := os.Stat(localPath); err == nil {
			return localPath, nil
		}
	}
	return c.Download(ctx, channelID, iconURL)
}

// Download fetches the icon at iconURL and stores it under the cache directory,
// returning the local file path.
func (c *Cache) Download(ctx context.Context, channelID, iconURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, iconURL, nil)
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Accept", "image/*,*/*")
	req.Header.Set("User-Agent", "xmltvguide/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("closing response body: %v", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d from %s", resp.StatusCode, iconURL)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading body: %w", err)
	}

	ext := extFromContentType(resp.Header.Get("Content-Type"))
	if ext == "" {
		ext = extFromURL(iconURL)
	}
	if ext == "" {
		ext = ".jpg"
	}

	dir := filepath.Join(c.dir, "channels")
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", fmt.Errorf("creating image directory: %w", err)
	}

	localPath := filepath.Join(dir, channelID+ext)
	if err := os.WriteFile(localPath, data, 0600); err != nil {
		return "", fmt.Errorf("writing icon file: %w", err)
	}

	return localPath, nil
}

// extFromContentType maps a MIME type to a file extension.
func extFromContentType(ct string) string {
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	switch ct {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/svg+xml", "image/svg":
		return ".svg"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}

// extFromURL extracts a recognised image file extension from a URL path,
// ignoring query parameters.
func extFromURL(u string) string {
	if i := strings.Index(u, "?"); i >= 0 {
		u = u[:i]
	}
	ext := filepath.Ext(u)
	switch strings.ToLower(ext) {
	case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp":
		return ext
	default:
		return ""
	}
}
