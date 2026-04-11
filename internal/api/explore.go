package api

import "net/http"

func (h *Handler) getNowNext(w http.ResponseWriter, r *http.Request) {
	entries, err := h.db.GetNowNext()
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, entries)
}
