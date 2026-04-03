package api

import (
	"context"
	"encoding/json"
	"math"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/acbgbca/xmltvguide/internal/database"
)

// store is the narrow interface the Handler needs from the database layer.
type store interface {
	GetChannels() ([]database.Channel, error)
	GetAirings(date time.Time) ([]database.Airing, error)
	GetStatus() database.Status
	EnsureChannelIcon(ctx context.Context, channelID string) (string, error)
	SearchSimple(query string, includeRepeats bool) ([]database.SearchResult, error)
	SearchAdvanced(query string, categories []string, includePast bool, includeRepeats bool) ([]database.SearchResult, error)
	GetCategories() ([]string, error)
}

// Handler holds the HTTP handler dependencies.
type Handler struct {
	db store
}

// New creates a new Handler backed by db.
func New(db store) *Handler {
	return &Handler{db: db}
}

// RegisterRoutes adds all API routes to mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/channels", h.getChannels)
	mux.HandleFunc("GET /api/guide", h.getGuide)
	mux.HandleFunc("GET /api/status", h.getStatus)
	mux.HandleFunc("GET /api/search", h.getSearch)
	mux.HandleFunc("GET /api/categories", h.getCategories)
	mux.HandleFunc("GET /images/channel/{id}", h.serveChannelIcon)
}

func (h *Handler) getChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := h.db.GetChannels()
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, channels)
}

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
	airings, err := h.db.GetAirings(date)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, airings)
}

func (h *Handler) getStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.db.GetStatus())
}

func (h *Handler) serveChannelIcon(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	localPath, err := h.db.EnsureChannelIcon(r.Context(), id)
	if err != nil {
		http.Error(w, "failed to retrieve icon", http.StatusInternalServerError)
		return
	}
	if localPath == "" {
		http.NotFound(w, r)
		return
	}

	data, err := os.ReadFile(localPath)
	if err != nil {
		http.Error(w, "failed to read icon", http.StatusInternalServerError)
		return
	}

	contentType := mime.TypeByExtension(filepath.Ext(localPath))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	w.Header().Set("Content-Type", contentType)
	w.Write(data) //nolint:errcheck
}

// searchResultAiring is the JSON shape for a single airing in a search result group.
type searchResultAiring struct {
	ChannelID         string    `json:"channelId"`
	ChannelName       string    `json:"channelName"`
	StartTime         time.Time `json:"startTime"`
	StopTime          time.Time `json:"stopTime"`
	SubTitle          string    `json:"subTitle,omitempty"`
	Description       string    `json:"description,omitempty"`
	EpisodeNumDisplay string    `json:"episodeNumDisplay,omitempty"`
	Categories        []string  `json:"categories,omitempty"`
	IsRepeat          bool      `json:"isRepeat"`
	IsPremiere        bool      `json:"isPremiere"`
}

// searchResultGroup is the JSON shape for a group of airings sharing the same title.
type searchResultGroup struct {
	Title   string               `json:"title"`
	Airings []searchResultAiring `json:"airings"`
}

func (h *Handler) getSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		http.Error(w, "missing required parameter: q", http.StatusBadRequest)
		return
	}

	mode := r.URL.Query().Get("mode")
	includeRepeats := r.URL.Query().Get("include_repeats") != "false"

	var results []database.SearchResult
	var err error

	if mode == "advanced" {
		var categories []string
		if cats := r.URL.Query().Get("categories"); cats != "" {
			categories = strings.Split(cats, ",")
		}
		includePast := r.URL.Query().Get("include_past") == "true"
		results, err = h.db.SearchAdvanced(q, categories, includePast, includeRepeats)
	} else {
		results, err = h.db.SearchSimple(q, includeRepeats)
	}

	if err != nil {
		http.Error(w, "search error", http.StatusInternalServerError)
		return
	}

	// Group by title, tracking best rank per group.
	type groupData struct {
		airings  []searchResultAiring
		bestRank float64
	}
	groups := map[string]*groupData{}
	order := []string{} // preserve insertion order for determinism

	for _, sr := range results {
		a := searchResultAiring{
			ChannelID:         sr.ChannelID,
			ChannelName:       sr.ChannelName,
			StartTime:         sr.Start,
			StopTime:          sr.Stop,
			SubTitle:          sr.SubTitle,
			Description:       sr.Description,
			EpisodeNumDisplay: sr.EpisodeNumDisplay,
			Categories:        sr.Categories,
			IsRepeat:          sr.IsRepeat,
			IsPremiere:        sr.IsPremiere,
		}

		g, ok := groups[sr.Title]
		if !ok {
			g = &groupData{bestRank: math.MaxFloat64}
			groups[sr.Title] = g
			order = append(order, sr.Title)
		}
		g.airings = append(g.airings, a)
		if sr.Rank < g.bestRank {
			g.bestRank = sr.Rank
		}
	}

	// Sort airings within each group by start time ascending.
	for _, g := range groups {
		sort.Slice(g.airings, func(i, j int) bool {
			return g.airings[i].StartTime.Before(g.airings[j].StartTime)
		})
	}

	// Sort groups by best rank, then alphabetically by title.
	sort.SliceStable(order, func(i, j int) bool {
		ri, rj := groups[order[i]].bestRank, groups[order[j]].bestRank
		if ri != rj {
			return ri < rj
		}
		return order[i] < order[j]
	})

	response := make([]searchResultGroup, 0, len(order))
	for _, title := range order {
		response = append(response, searchResultGroup{
			Title:   title,
			Airings: groups[title].airings,
		})
	}

	writeJSON(w, response)
}

func (h *Handler) getCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := h.db.GetCategories()
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, cats)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
