package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/acbgbca/xmltvguide/internal/store"
)

// Handler holds the HTTP handler dependencies.
type Handler struct {
	store *store.Store
}

// New creates a new Handler backed by s.
func New(s *store.Store) *Handler {
	return &Handler{store: s}
}

// RegisterRoutes adds all API routes to mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/channels", h.getChannels)
	mux.HandleFunc("GET /api/guide", h.getGuide)
	mux.HandleFunc("GET /api/status", h.getStatus)
}

// getChannels returns the full channel list in source order.
func (h *Handler) getChannels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.store.GetChannels())
}

// getGuide returns all programmes for a given date.
// The date is passed as a query parameter: ?date=YYYY-MM-DD
// If omitted, today's date (server local time) is used.
func (h *Handler) getGuide(w http.ResponseWriter, r *http.Request) {
	dateStr := r.URL.Query().Get("date")
	var date time.Time
	if dateStr == "" {
		date = time.Now()
	} else {
		var err error
		date, err = time.ParseInLocation("2006-01-02", dateStr, time.Local)
		if err != nil {
			http.Error(w, "invalid date, expected YYYY-MM-DD", http.StatusBadRequest)
			return
		}
	}
	writeJSON(w, h.store.GetProgrammes(date))
}

// getStatus returns the last and next refresh times and the source URL.
func (h *Handler) getStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.store.GetStatus())
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
