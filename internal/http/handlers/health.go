package handlers

import (
	"context"
	"net/http"
	"time"

	httputil "memorybutton/backend/internal/httputil"
)

// GET /api/v1/health (и /health без префикса)
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	dbOK := h.Store.Ping(ctx) == nil
	status := "ok"
	code := http.StatusOK
	if !dbOK {
		status = "degraded"
		code = http.StatusServiceUnavailable
	}
	httputil.WriteJSON(w, r, code, map[string]any{
		"status":   status,
		"database": dbOK,
		"ai":       h.AI.Name,
		"time":     time.Now().UTC(),
	})
}
