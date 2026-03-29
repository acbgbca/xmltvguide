package store

import (
	"sync"
	"time"

	"github.com/acbgbca/xmltvguide/internal/xmltv"
)

// Channel holds the display data for a single TV channel.
type Channel struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Icon        string `json:"icon,omitempty"`
}

// Programme holds the data for a single broadcast programme.
type Programme struct {
	ChannelID   string    `json:"channelId"`
	Title       string    `json:"title"`
	SubTitle    string    `json:"subTitle,omitempty"`
	Start       time.Time `json:"start"`
	Stop        time.Time `json:"stop"`
	Description string    `json:"description,omitempty"`
	Categories  []string  `json:"categories,omitempty"`
}

// Status holds metadata about the last data refresh.
type Status struct {
	LastRefresh time.Time `json:"lastRefresh"`
	NextRefresh time.Time `json:"nextRefresh"`
	SourceURL   string    `json:"sourceUrl"`
}

// Store is a thread-safe in-memory store for XMLTV channel and programme data.
type Store struct {
	mu          sync.RWMutex
	channels    []Channel
	programmes  []Programme
	lastRefresh time.Time
	nextRefresh time.Time
	sourceURL   string
}

// New creates a new empty Store.
func New(sourceURL string) *Store {
	return &Store{sourceURL: sourceURL}
}

// SetData atomically replaces all stored data with the contents of the
// provided XMLTV document. Channel and programme order from the source is preserved.
func (s *Store) SetData(tv *xmltv.TV, nextRefresh time.Time) {
	channels := make([]Channel, 0, len(tv.Channels))
	for _, ch := range tv.Channels {
		c := Channel{ID: ch.ID}
		if len(ch.DisplayNames) > 0 {
			c.DisplayName = ch.DisplayNames[0].Value
		}
		if c.DisplayName == "" {
			c.DisplayName = ch.ID
		}
		if len(ch.Icons) > 0 {
			c.Icon = ch.Icons[0].Src
		}
		channels = append(channels, c)
	}

	programmes := make([]Programme, 0, len(tv.Programmes))
	for _, p := range tv.Programmes {
		prog := Programme{
			ChannelID: p.Channel,
			Start:     p.Start.Time,
			Stop:      p.Stop.Time,
		}
		if len(p.Titles) > 0 {
			prog.Title = p.Titles[0].Value
		}
		if len(p.SubTitles) > 0 {
			prog.SubTitle = p.SubTitles[0].Value
		}
		if len(p.Descs) > 0 {
			prog.Description = p.Descs[0].Value
		}
		for _, cat := range p.Categories {
			if cat.Value != "" {
				prog.Categories = append(prog.Categories, cat.Value)
			}
		}
		programmes = append(programmes, prog)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.channels = channels
	s.programmes = programmes
	s.lastRefresh = time.Now()
	s.nextRefresh = nextRefresh
}

// GetChannels returns a copy of all channels in source order.
func (s *Store) GetChannels() []Channel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Channel, len(s.channels))
	copy(result, s.channels)
	return result
}

// GetProgrammes returns all programmes that overlap with the given calendar
// date, interpreted in the server's local timezone.
func (s *Store) GetProgrammes(date time.Time) []Programme {
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.Local)
	dayEnd := dayStart.Add(24 * time.Hour)

	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]Programme, 0)
	for _, p := range s.programmes {
		if p.Stop.After(dayStart) && p.Start.Before(dayEnd) {
			result = append(result, p)
		}
	}
	return result
}

// GetStatus returns the current refresh metadata.
func (s *Store) GetStatus() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Status{
		LastRefresh: s.lastRefresh,
		NextRefresh: s.nextRefresh,
		SourceURL:   s.sourceURL,
	}
}
