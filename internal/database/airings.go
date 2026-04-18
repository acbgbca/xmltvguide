package database

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/acbgbca/xmltvguide/internal/model"
	"github.com/acbgbca/xmltvguide/internal/xmltv"
)

// GetAirings returns all airings that overlap with the given calendar date,
// interpreted in the server's local timezone.
func (d *DB) GetAirings(ctx context.Context, date time.Time) ([]model.Airing, error) {
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.Local).UTC()
	dayEnd := dayStart.Add(24 * time.Hour)

	rows, err := d.db.QueryContext(ctx, `
		SELECT
			channel_id,
			start_time,
			stop_time,
			title,
			COALESCE(sub_title, ''),
			COALESCE(description, ''),
			COALESCE(categories, '[]'),
			COALESCE(episode_num, ''),
			COALESCE(episode_num_display, ''),
			COALESCE(prog_id, ''),
			COALESCE(star_rating, ''),
			COALESCE(content_rating, ''),
			COALESCE(year, ''),
			COALESCE(icon, ''),
			COALESCE(country, ''),
			is_repeat,
			is_premiere
		FROM airings
		WHERE stop_time > ? AND start_time < ?
		ORDER BY start_time
	`, dayStart.Format(time.RFC3339), dayEnd.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("querying airings: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("closing airing rows: %v", err)
		}
	}()

	airings := []model.Airing{}
	for rows.Next() {
		var a model.Airing
		var startStr, stopStr, catsJSON string
		var isRepeat, isPremiere int

		if err := rows.Scan(
			&a.ChannelID, &startStr, &stopStr,
			&a.Title, &a.SubTitle, &a.Description, &catsJSON,
			&a.EpisodeNum, &a.EpisodeNumDisplay, &a.ProgID,
			&a.StarRating, &a.ContentRating, &a.Year, &a.Icon, &a.Country,
			&isRepeat, &isPremiere,
		); err != nil {
			return nil, fmt.Errorf("scanning airing: %w", err)
		}

		a.Start, _ = time.Parse(time.RFC3339, startStr)
		a.Stop, _ = time.Parse(time.RFC3339, stopStr)
		json.Unmarshal([]byte(catsJSON), &a.Categories) //nolint:errcheck // malformed JSON yields nil slice, which is acceptable
		a.IsRepeat = isRepeat == 1
		a.IsPremiere = isPremiere == 1

		airings = append(airings, a)
	}
	return airings, rows.Err()
}

// airingFromXMLTV maps an xmltv.Programme to a model.Airing.
// Complexity comes from the number of optional XMLTV fields — not reducible without obfuscating the mapping.
//
//nolint:cyclop
func airingFromXMLTV(p xmltv.Programme) model.Airing {
	a := model.Airing{
		ChannelID:  p.Channel,
		Start:      p.Start.Time,
		Stop:       p.Stop.Time,
		IsRepeat:   p.PreviouslyShown != nil,
		IsPremiere: p.Premiere != nil || p.New != nil,
	}

	if len(p.Titles) > 0 {
		a.Title = p.Titles[0].Value
	}
	if len(p.SubTitles) > 0 {
		a.SubTitle = p.SubTitles[0].Value
	}
	if len(p.Descs) > 0 {
		a.Description = p.Descs[0].Value
	}
	for _, cat := range p.Categories {
		if cat.Value != "" {
			a.Categories = append(a.Categories, cat.Value)
		}
	}
	for _, en := range p.EpisodeNums {
		switch strings.ToLower(en.System) {
		case "xmltv_ns":
			a.EpisodeNum = strings.TrimSpace(en.Value)
		case "onscreen", "sxxexx":
			a.EpisodeNumDisplay = strings.TrimSpace(en.Value)
		case "dd_progid":
			a.ProgID = strings.TrimSpace(en.Value)
		}
	}
	if len(p.StarRatings) > 0 {
		a.StarRating = p.StarRatings[0].Value
	}
	if len(p.Ratings) > 0 {
		a.ContentRating = p.Ratings[0].Value
	}
	if p.Date != "" {
		// Date may be a full date string — we only want the 4-digit year.
		a.Year = p.Date
		if len(p.Date) > 4 {
			a.Year = p.Date[:4]
		}
	}
	if len(p.Icons) > 0 {
		a.Icon = p.Icons[0].Src
	}
	if len(p.Country) > 0 {
		a.Country = p.Country[0].Value
	}

	return a
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
