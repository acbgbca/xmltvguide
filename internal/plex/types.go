// Package plex contains a typed HTTP client for the subset of the Plex
// Media Server EPG API used by this app.
package plex

// DVR represents a single DVR configured on a Plex Media Server. The minimal
// fields here are what the EPG enrichment poller needs: an identifier, the
// lineup IDs the DVR draws from, and the path prefix used to query the
// EPG grid (`{GridKey}/grid?...`).
type DVR struct {
	ID        int      `json:"id"`
	LineupIDs []string `json:"lineupIDs,omitempty"`
	GridKey   string   `json:"gridKey,omitempty"`
}

// LineupChannel describes one channel within a Plex EPG lineup.
type LineupChannel struct {
	ID          string `json:"id"`
	XMLTVID     string `json:"xmltvId,omitempty"`
	LCN         string `json:"lcn,omitempty"`
	DisplayName string `json:"title,omitempty"`
}

// GridEntry describes a single airing returned by the EPG grid endpoint.
// BeginsAt and EndsAt are unix-seconds timestamps as returned by Plex.
type GridEntry struct {
	RatingKey string `json:"ratingKey"`
	Channel   string `json:"channel,omitempty"`
	BeginsAt  int64  `json:"beginsAt"`
	EndsAt    int64  `json:"endsAt"`
	Title     string `json:"title,omitempty"`
	DdProgID  string `json:"ddProgID,omitempty"`
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
