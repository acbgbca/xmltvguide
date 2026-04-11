package model

import "time"

// Channel holds display data for a TV channel.
type Channel struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Icon        string `json:"icon,omitempty"`
	LCN         *int   `json:"lcn,omitempty"`
}

// Airing holds all data for a single broadcast slot.
// JSON field names for start/stop are kept as "start"/"stop" for frontend compatibility.
type Airing struct {
	ChannelID         string    `json:"channelId"`
	Start             time.Time `json:"start"`
	Stop              time.Time `json:"stop"`
	Title             string    `json:"title"`
	SubTitle          string    `json:"subTitle,omitempty"`
	Description       string    `json:"description,omitempty"`
	Categories        []string  `json:"categories,omitempty"`
	EpisodeNum        string    `json:"episodeNum,omitempty"`
	EpisodeNumDisplay string    `json:"episodeNumDisplay,omitempty"`
	ProgID            string    `json:"progId,omitempty"`
	StarRating        string    `json:"starRating,omitempty"`
	ContentRating     string    `json:"contentRating,omitempty"`
	Year              string    `json:"year,omitempty"`
	Icon              string    `json:"icon,omitempty"`
	Country           string    `json:"country,omitempty"`
	IsRepeat          bool      `json:"isRepeat"`
	IsPremiere        bool      `json:"isPremiere"`
}

// Status holds metadata about the last data refresh.
type Status struct {
	LastRefresh time.Time `json:"lastRefresh"`
	NextRefresh time.Time `json:"nextRefresh"`
	SourceURL   string    `json:"sourceUrl"`
}

// SearchResult extends Airing with additional fields needed by the search API.
type SearchResult struct {
	Airing
	ChannelName string  `json:"channelName"`
	Rank        float64 `json:"-"`
}

// NowNextEntry holds the current and next airing for a single channel.
type NowNextEntry struct {
	ChannelID   string   `json:"channelId"`
	ChannelName string   `json:"channelName"`
	Current     *Airing  `json:"current"`
	Next        *Airing  `json:"next"`
}
