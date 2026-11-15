package handlers

import (
	"net/http"

	httputil "memorybutton/backend/internal/httputil"
	"memorybutton/backend/internal/domain"
)

// GET /api/v1/questions?person_id=&status=
func (h *Handler) ListQuestions(w http.ResponseWriter, r *http.Request) {
	personID := r.URL.Query().Get("person_id")
	if personID != "" && !validUUID(personID) {
		handleErr(w, r, badRequest("person_id должен быть UUID"))
		return
	}
	qs, total, err := h.Store.ListQuestions(r.Context(), domain.QuestionFilter{
		PersonID: personID,
		Status:   r.URL.Query().Get("status"),
		Source:   r.URL.Query().Get("source"),
		Limit:    queryInt(r, "limit", 100),
		Offset:   queryInt(r, "offset", 0),
	})
	if err != nil {
		handleErr(w, r, err)
		return
	}
	httputil.WriteJSON(w, r, http.StatusOK, map[string]any{"questions": qs, "total": total})
}

// POST /api/v1/questions
func (h *Handler) CreateQuestion(w http.ResponseWriter, r *http.Request) {
	var req CreateQuestionRequest
	if err := decodeJSON(r, &req); err != nil {
		handleErr(w, r, err)
		return
	}
	if trimLower(req.Text) == "" {
		handleErr(w, r, badRequest("text обязателен"))
		return
	}
	q := &domain.Question{
		PersonID:         req.PersonID,
		Text:             req.Text,
		Source:           domain.SourceManual,
		Reason:           req.Reason,
		Status:           domain.QuestionOpen,
		Priority:         req.Priority,
		RelatedFactID:    req.RelatedFactID,
		RelatedSessionID: req.RelatedSessionID,
	}
	if err := h.Store.CreateQuestion(r.Context(), q); err != nil {
		handleErr(w, r, err)
		return
	}
	httputil.WriteJSON(w, r, http.StatusCreated, q)
}

// PATCH /api/v1/questions/{id}
func (h *Handler) UpdateQuestion(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		handleErr(w, r, err)
		return
	}
	var req UpdateQuestionRequest
	if err := decodeJSON(r, &req); err != nil {
		handleErr(w, r, err)
		return
	}
	q, err := h.Store.GetQuestion(r.Context(), id)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	if req.Text != nil {
		q.Text = *req.Text
	}
	if req.Status != nil {
		q.Status = domain.QuestionStatus(*req.Status)
	}
	if req.Priority != nil {
		q.Priority = *req.Priority
	}
	if req.Reason != nil {
		q.Reason = *req.Reason
	}
	if err := h.Store.UpdateQuestion(r.Context(), &q); err != nil {
		handleErr(w, r, err)
		return
	}
	httputil.WriteJSON(w, r, http.StatusOK, q)
}

// GET /api/v1/persons/{id}/questions
func (h *Handler) ListPersonQuestions(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		handleErr(w, r, err)
		return
	}
	qs, total, err := h.Store.ListQuestions(r.Context(), domain.QuestionFilter{
		PersonID: id,
		Status:   r.URL.Query().Get("status"),
		Limit:    queryInt(r, "limit", 100),
		Offset:   queryInt(r, "offset", 0),
	})
	if err != nil {
		handleErr(w, r, err)
		return
	}
	httputil.WriteJSON(w, r, http.StatusOK, map[string]any{"questions": qs, "total": total})
}
