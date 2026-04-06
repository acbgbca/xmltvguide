package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
)

const maxDebugBodyBytes = 64 * 1024 // 64 KB

type debugLogRequest struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Source  string `json:"source"`
	Lineno  int    `json:"lineno"`
	Colno   int    `json:"colno"`
	Stack   string `json:"stack"`
	URL     string `json:"url"`
}

func (h *Handler) postDebugLog(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxDebugBodyBytes+1))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	if int64(len(body)) > maxDebugBodyBytes {
		http.Error(w, "request body too large", http.StatusBadRequest)
		return
	}

	var req debugLogRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("[client-error] type=%s message=%q source=%s lineno=%d colno=%d url=%s stack=%q",
		req.Type, req.Message, req.Source, req.Lineno, req.Colno, req.URL, req.Stack)

	w.WriteHeader(http.StatusNoContent)
}
