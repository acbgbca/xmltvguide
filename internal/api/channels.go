package api

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
)

func (h *Handler) getChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := h.db.GetChannels(r.Context())
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, channels)
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
	w.Write(data) //nolint:errcheck // nosemgrep: go.lang.security.audit.xss.no-direct-write-to-responsewriter.no-direct-write-to-responsewriter
}
