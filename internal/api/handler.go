package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/acbgbca/xmltvguide/internal/model"
)

// store is the narrow interface the Handler needs from the database layer.
type store interface {
	GetChannels() ([]model.Channel, error)
	GetAirings(date time.Time) ([]model.Airing, error)
	GetStatus() model.Status
	EnsureChannelIcon(ctx context.Context, channelID string) (string, error)
	SearchSimple(query string, includeRepeats bool, today bool) ([]model.SearchResult, error)
	SearchAdvanced(query string, categories []string, includePast bool, includeRepeats bool, today bool) ([]model.SearchResult, error)
	SearchBrowse(categories []string, isPremiere bool, includePast bool, includeRepeats bool, today bool) ([]model.SearchResult, error)
	GetCategories() ([]string, error)
	GetNowNext() ([]model.NowNextEntry, error)
}

// Handler holds the HTTP handler dependencies.
type Handler struct {
	db     store
	rssTTL int // default RSS TTL in minutes; 0 means use hard-coded default (360)
}

// New creates a new Handler backed by db. rssTTL is the server-wide default
// RSS feed TTL in minutes (from the RSS_TTL env var); pass 0 to use the
// hard-coded default of 360 minutes.
func New(db store, rssTTL int) *Handler {
	return &Handler{db: db, rssTTL: rssTTL}
}

// RegisterRoutes adds all API routes to mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/channels", h.getChannels)
	mux.HandleFunc("GET /api/guide", h.getGuide)
	mux.HandleFunc("GET /api/status", h.getStatus)
	mux.HandleFunc("GET /api/search", h.getSearch)
	mux.HandleFunc("GET /api/categories", h.getCategories)
	mux.HandleFunc("GET /api/explore/now-next", h.getNowNext)
	mux.HandleFunc("GET /images/channel/{id}", h.serveChannelIcon)
	mux.HandleFunc("POST /api/debug/log", h.postDebugLog)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
