package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/acbgbca/xmltvguide/internal/database"
)

// store is the narrow interface the Handler needs from the database layer.
type store interface {
	GetChannels() ([]database.Channel, error)
	GetAirings(date time.Time) ([]database.Airing, error)
	GetStatus() database.Status
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
