package database

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// EnsureChannelIcon ensures the channel's icon is present on disk and returns
// its local file path. Returns ("", nil) when the channel has no icon or does
// not exist. Re-downloads from the stored external URL if the cached file is
// missing.
func (d *DB) EnsureChannelIcon(ctx context.Context, channelID string) (string, error) {
	var localPath, iconURL string
	err := d.db.QueryRowContext(ctx,
		`SELECT COALESCE(icon, ''), COALESCE(icon_url, '') FROM channels WHERE id = ?`,
		channelID,
	).Scan(&localPath, &iconURL)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("querying channel icon: %w", err)
	}
	if iconURL == "" {
		return "", nil
	}

	// Return the cached path if the file still exists.
	if localPath != "" {
		if _, err := os.Stat(localPath); err == nil {
			return localPath, nil
		}
	}

	// File is missing — re-download.
	if d.httpClient == nil || d.imageCacheDir == "" {
		return "", fmt.Errorf("image cache not configured for re-download")
	}
	newPath, err := d.downloadAndSaveIcon(ctx, channelID, iconURL)
	if err != nil {
		return "", fmt.Errorf("re-downloading icon for %s: %w", channelID, err)
	}

	// Update the stored local path.
	if _, err := d.db.ExecContext(ctx,
		`UPDATE channels SET icon = ? WHERE id = ?`, newPath, channelID,
	); err != nil {
		log.Printf("warning: failed to update icon path for channel %s: %v", channelID, err)
	}

	return newPath, nil
}

// downloadAndSaveIcon fetches the image at iconURL, determines its file
// extension from the Content-Type header (falling back to the URL extension or
// ".jpg"), writes it to {imageCacheDir}/channels/{channelID}{ext}, and returns
// the local path.
func (d *DB) downloadAndSaveIcon(ctx context.Context, channelID, iconURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, iconURL, nil)
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Accept", "image/*,*/*")
	req.Header.Set("User-Agent", "xmltvguide/1.0")
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading: %w", err)
	}
	defer resp.Body.Close()
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

	dir := filepath.Join(d.imageCacheDir, "channels")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating image directory: %w", err)
	}

	localPath := filepath.Join(dir, channelID+ext)
	if err := os.WriteFile(localPath, data, 0644); err != nil {
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
