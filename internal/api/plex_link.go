package api

import (
	"net/http"
	"net/url"
	"strings"
)

// plexLinkView is the JSON payload for GET /api/channels/{id}/plex-link.
// Both URLs target the same Plex Live TV channel; the frontend chooses which
// to open based on a "Watch in Plex Web" / "Open in Plex App" button.
type plexLinkView struct {
	WebURL string `json:"webUrl"`
	AppURL string `json:"appUrl"`
}

// SetPlexLinkURL configures the Plex base URL used to build "Watch now"
// deep links. Pass the user-facing URL (PLEX_EXTERNAL_URL, falling back to
// PLEX_URL). Empty disables the endpoint — it returns 404 in that case.
func (h *Handler) SetPlexLinkURL(externalURL string) {
	h.plexLinkURL = externalURL
}

func (h *Handler) getPlexLink(w http.ResponseWriter, r *http.Request) {
	if h.plexLinkURL == "" {
		http.NotFound(w, r)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	channels, err := h.db.GetChannels(r.Context())
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	for i := range channels {
		c := channels[i]
		if c.ID != id {
			continue
		}
		if c.PlexChannelID == nil || c.PlexLineupID == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, buildPlexLinks(h.plexLinkURL, *c.PlexLineupID, *c.PlexChannelID))
		return
	}
	http.NotFound(w, r)
}

// buildPlexLinks constructs the web + app deep-link URLs for one Plex channel.
// base is trimmed of any trailing slash so the result never contains "//".
// lineupID often contains a colon (e.g. tv.plex.providers.epg.cloud:4) which
// url.PathEscape encodes for safe inclusion in a URL path.
func buildPlexLinks(base, lineupID, channelID string) plexLinkView {
	base = strings.TrimRight(base, "/")
	encLineup := url.PathEscape(lineupID)
	encChannel := url.PathEscape(channelID)
	return plexLinkView{
		WebURL: base + "/web/index.html#!/livetv/" + encLineup + "/channels/" + encChannel,
		AppURL: "plex://livetv?lineup=" + encLineup + "&channel=" + encChannel,
	}
}
