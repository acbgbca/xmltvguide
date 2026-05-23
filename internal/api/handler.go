package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/acbgbca/xmltvguide/internal/deepcheck"
	"github.com/acbgbca/xmltvguide/internal/model"
)

// store is the narrow interface the Handler needs from the database layer.
type store interface {
	GetChannels(ctx context.Context) ([]model.Channel, error)
	GetAirings(ctx context.Context, date time.Time) ([]model.Airing, error)
	GetStatus() model.Status
	EnsureChannelIcon(ctx context.Context, channelID string) (string, error)
	SearchSimple(ctx context.Context, query string, includeRepeats bool, today bool) ([]model.SearchResult, error)
	SearchAdvanced(ctx context.Context, query string, categories []string, includePast bool, includeRepeats bool, today bool) ([]model.SearchResult, error)
	SearchBrowse(ctx context.Context, categories []string, isPremiere bool, includePast bool, includeRepeats bool, today bool) ([]model.SearchResult, error)
	GetCategories(ctx context.Context) ([]string, error)
	GetNowNext(ctx context.Context) ([]model.NowNextEntry, error)
	Ping(ctx context.Context) error
	DeepCheck(ctx context.Context) deepcheck.DBCheckResults
}

// DeepCheckConfig bundles the deepcheck-specific dependencies for the Handler
// constructor so the signature stays manageable.
type DeepCheckConfig struct {
	HTTPClient    *http.Client
	XMLTVURL      string
	PollInterval  time.Duration
	DBPath        string
	ImageCacheDir string
	PlexURL       string
	PlexClient    deepcheck.PlexProbe
}

// Handler holds the HTTP handler dependencies.
type Handler struct {
	db           store
	rssTTL       int          // default RSS TTL in minutes; 0 means use hard-coded default (360)
	refreshFn    func() error // optional; nil means refresh endpoint returns 501
	deep         DeepCheckConfig
	plexStatusFn PlexStatusFunc // optional; nil means /api/plex/status returns {"enabled": false}
}

// New creates a new Handler backed by db. rssTTL is the server-wide default
// RSS feed TTL in minutes (from the RSS_TTL env var); pass 0 to use the
// hard-coded default of 360 minutes. refreshFn is called by POST /api/guide/refresh;
// pass nil to disable the endpoint. deepCfg supplies the dependencies needed
// by the /api/deepcheck endpoint.
func New(db store, rssTTL int, refreshFn func() error, deepCfg DeepCheckConfig) *Handler {
	return &Handler{db: db, rssTTL: rssTTL, refreshFn: refreshFn, deep: deepCfg}
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
	mux.HandleFunc("POST /api/guide/refresh", h.postGuideRefresh)
	mux.HandleFunc("GET /api/health", h.getHealth)
	mux.HandleFunc("GET /api/deepcheck", h.getDeepCheck)
	mux.HandleFunc("GET /api/plex/status", h.getPlexStatus)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
