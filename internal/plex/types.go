// Package plex contains a typed HTTP client for the subset of the Plex
// Media Server EPG API used by this app.
package plex

// DVR represents a single DVR configured on a Plex Media Server. Identity is a
// single `epgIdentifier` (e.g. `tv.plex.providers.epg.cloud:4`) — Plex Cloud
// EPG returns one DVR ↔ one EPG identifier, which is also the path prefix used
// for the channels and grid endpoints.
type DVR struct {
	Key           string `json:"key"`
	EPGIdentifier string `json:"epgIdentifier"`
}

// LineupChannel describes one channel within a Plex EPG lineup. `vcn` is the
// Plex equivalent of XMLTV's LCN — a zero-padded string like "001" or "010".
// CallSign is captured for diagnostics but not used for matching.
type LineupChannel struct {
	ID          string `json:"id"`
	VCN         string `json:"vcn,omitempty"`
	DisplayName string `json:"title,omitempty"`
	CallSign    string `json:"callSign,omitempty"`
}

// GridMedia is the per-broadcast slot info under a grid entry's Media[]. Real
// Plex responses always have exactly one entry here for EPG queries; we
// nevertheless model it as a slice to mirror the JSON.
type GridMedia struct {
	ChannelIdentifier string `json:"channelIdentifier"`
	BeginsAt          int64  `json:"beginsAt"`
	EndsAt            int64  `json:"endsAt"`
}

// GridEntry describes a single airing returned by the EPG grid endpoint. The
// channel and timing info lives inside Media[0]; use PrimaryMedia to access it.
type GridEntry struct {
	RatingKey string      `json:"ratingKey"`
	Title     string      `json:"title,omitempty"`
	Media     []GridMedia `json:"Media,omitempty"`
}

// PrimaryMedia returns the first Media block, or ok=false when Media is empty.
func (g GridEntry) PrimaryMedia() (GridMedia, bool) {
	if len(g.Media) == 0 {
		return GridMedia{}, false
	}
	return g.Media[0], true
}

// Internal response wrappers — Plex wraps all responses in a MediaContainer.

type dvrsResponse struct {
	MediaContainer struct {
		Dvr []DVR `json:"Dvr"`
	} `json:"MediaContainer"`
}

type lineupChannelsResponse struct {
	MediaContainer struct {
		Channel []LineupChannel `json:"Channel"`
	} `json:"MediaContainer"`
}

type gridResponse struct {
	MediaContainer struct {
		Metadata []GridEntry `json:"Metadata"`
	} `json:"MediaContainer"`
}
