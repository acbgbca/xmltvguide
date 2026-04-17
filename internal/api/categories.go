package api

import "net/http"

func (h *Handler) getCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := h.db.GetCategories(r.Context())
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, cats)
}
