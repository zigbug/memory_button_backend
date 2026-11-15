package handlers

import (
	"net/http"

	httputil "memorybutton/backend/internal/httputil"
	"memorybutton/backend/internal/domain"
)

// GET /api/v1/persons/{id}/stories
func (h *Handler) ListPersonStories(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		handleErr(w, r, err)
		return
	}
	stories, err := h.Store.ListStoriesByPerson(r.Context(), id)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	httputil.WriteJSON(w, r, http.StatusOK, map[string]any{"stories": stories})
}

// GET /api/v1/stories/{id}
func (h *Handler) GetStory(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		handleErr(w, r, err)
		return
	}
	story, err := h.Store.GetStory(r.Context(), id)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	httputil.WriteJSON(w, r, http.StatusOK, story)
}

// POST /api/v1/stories/{id}/confirm
func (h *Handler) ConfirmStory(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		handleErr(w, r, err)
		return
	}
	var req ConfirmStoryRequest
	if err := decodeJSON(r, &req); err != nil {
		handleErr(w, r, err)
		return
	}
	story, err := h.Store.GetStory(r.Context(), id)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	if story.Status == domain.StoryConfirmed && req.Text == "" && len(req.PersonIDs) == 0 {
		httputil.WriteJSON(w, r, http.StatusOK, story)
		return
	}
	if req.Text != "" {
		if err := h.Store.UpdateStoryText(r.Context(), id, req.Text, story.Status == domain.StoryConfirmed); err != nil {
			handleErr(w, r, err)
			return
		}
	}
	if len(req.PersonIDs) > 0 {
		if err := h.Store.ReplaceStoryPersons(r.Context(), id, req.PersonIDs); err != nil {
			handleErr(w, r, err)
			return
		}
	}
	if err := h.Store.SetStoryStatus(r.Context(), id, domain.StoryConfirmed); err != nil {
		handleErr(w, r, err)
		return
	}
	updated, err := h.Store.GetStory(r.Context(), id)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	httputil.WriteJSON(w, r, http.StatusOK, updated)
}
