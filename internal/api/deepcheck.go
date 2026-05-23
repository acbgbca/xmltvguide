package api

import (
	"context"
	"net/http"
	"time"

	"github.com/acbgbca/xmltvguide/internal/deepcheck"
)

// deepCheckOverallTimeout caps the total time the deepcheck handler may run,
// independent of any per-check timeouts.
const deepCheckOverallTimeout = 15 * time.Second

func (h *Handler) getDeepCheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), deepCheckOverallTimeout)
	defer cancel()

	checker := &deepcheck.Checker{
		DB:            h.db,
		HTTPClient:    h.deep.HTTPClient,
		XMLTVURL:      h.deep.XMLTVURL,
		PollInterval:  h.deep.PollInterval,
		DBPath:        h.deep.DBPath,
		ImageCacheDir: h.deep.ImageCacheDir,
		PlexURL:       h.deep.PlexURL,
		PlexClient:    h.deep.PlexClient,
	}
	report := checker.Run(ctx)
	if report.Status != deepcheck.StatusSuccess {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	writeJSON(w, report)
}
