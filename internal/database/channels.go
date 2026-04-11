package database

import (
	"database/sql"
	"fmt"

	"github.com/acbgbca/xmltvguide/internal/model"
)

// GetChannels returns all channels ordered by their source sort order.
// The Icon field contains the proxy URL (/images/channel/{id}) for channels
// that have an icon; it is empty for channels without one.
// Channels matching the DB's hiddenIDs or hiddenLCNs are excluded.
func (d *DB) GetChannels() ([]model.Channel, error) {
	hiddenSQL, hiddenArgs := d.channelHiddenSQL("id", "lcn")
	q := `
		SELECT id, display_name, COALESCE(icon_url, ''), lcn
		FROM channels
		WHERE 1=1` + hiddenSQL + `
		ORDER BY sort_order
	`
	rows, err := d.db.Query(q, hiddenArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying channels: %w", err)
	}
	defer rows.Close()

	channels := []model.Channel{}
	for rows.Next() {
		var ch model.Channel
		var lcn sql.NullInt64
		var iconURL string
		if err := rows.Scan(&ch.ID, &ch.DisplayName, &iconURL, &lcn); err != nil {
			return nil, fmt.Errorf("scanning channel: %w", err)
		}
		if iconURL != "" {
			ch.Icon = "/images/channel/" + ch.ID
		}
		if lcn.Valid {
			n := int(lcn.Int64)
			ch.LCN = &n
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}
