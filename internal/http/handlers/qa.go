package handlers

import (
	"net/http"

	httputil "memorybutton/backend/internal/httputil"
)

// POST /api/v1/qa/ask
func (h *Handler) AskQA(w http.ResponseWriter, r *http.Request) {
	var req QARequest
	if err := decodeJSON(r, &req); err != nil {
		handleErr(w, r, err)
		return
	}
	if trimLower(req.Question) == "" {
		handleErr(w, r, badRequest("question обязателен"))
		return
	}
	if req.ScopePersonID != nil && !validUUID(*req.ScopePersonID) {
		handleErr(w, r, badRequest("scope_person_id должен быть UUID"))
		return
	}
	ans, err := h.QA.Ask(r.Context(), req.Question, req.ScopePersonID)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	httputil.WriteJSON(w, r, http.StatusOK, ans)
}
