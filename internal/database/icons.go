package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
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

	if d.imageCache == nil {
		return "", fmt.Errorf("image cache not configured for re-download")
	}

	newPath, err := d.imageCache.EnsureIcon(ctx, channelID, localPath, iconURL)
	if err != nil {
		return "", fmt.Errorf("re-downloading icon for %s: %w", channelID, err)
	}

	// Update the stored local path if it changed.
	if newPath != localPath {
		if _, err := d.db.ExecContext(ctx,
			`UPDATE channels SET icon = ? WHERE id = ?`, newPath, channelID,
		); err != nil {
			log.Printf("warning: failed to update icon path for channel %s: %v", channelID, err)
		}
	}

	return newPath, nil
}
