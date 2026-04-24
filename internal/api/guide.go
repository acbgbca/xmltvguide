package api

import (
	"net/http"
	"time"
)

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
	airings, err := h.db.GetAirings(r.Context(), date)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, airings)
}

func (h *Handler) getStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.db.GetStatus())
}

func (h *Handler) postGuideRefresh(w http.ResponseWriter, r *http.Request) {
	if h.refreshFn == nil {
		http.Error(w, "refresh not configured", http.StatusNotImplemented)
		return
	}
	if r.URL.Query().Get("sync") == "true" {
		if err := h.refreshFn(); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	go h.refreshFn() //nolint:errcheck
	w.WriteHeader(http.StatusAccepted)
}
