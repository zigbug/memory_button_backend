package handlers

import (
	"net/http"

	httputil "memorybutton/backend/internal/httputil"
	"memorybutton/backend/internal/domain"
)

// GET /api/v1/relationships?person_id=
func (h *Handler) ListRelationships(w http.ResponseWriter, r *http.Request) {
	personID := r.URL.Query().Get("person_id")
	if personID != "" && !validUUID(personID) {
		handleErr(w, r, badRequest("person_id должен быть UUID"))
		return
	}
	rels, err := h.Store.ListRelationships(r.Context(), personID)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	httputil.WriteJSON(w, r, http.StatusOK, map[string]any{"relationships": rels})
}

// POST /api/v1/relationships
func (h *Handler) CreateRelationship(w http.ResponseWriter, r *http.Request) {
	var req RelationshipRequest
	if err := decodeJSON(r, &req); err != nil {
		handleErr(w, r, err)
		return
	}
	if !validUUID(req.FromPersonID) || !validUUID(req.ToPersonID) {
		handleErr(w, r, badRequest("from_person_id и to_person_id должны быть UUID"))
		return
	}
	if req.FromPersonID == req.ToPersonID {
		handleErr(w, r, badRequest("связь человека с самим собой недопустима"))
		return
	}
	switch domain.RelationshipType(req.Type) {
	case domain.RelParentChild, domain.RelSpouse, domain.RelSibling, domain.RelOther:
	default:
		handleErr(w, r, badRequest("type: parent_child|spouse|sibling|other"))
		return
	}
	rel := req.toDomain("user")
	if err := h.Store.CreateRelationship(r.Context(), &rel); err != nil {
		handleErr(w, r, err)
		return
	}
	// Подтянем имена.
	if got, err := h.Store.GetRelationship(r.Context(), rel.ID); err == nil {
		rel = got
	}
	httputil.WriteJSON(w, r, http.StatusCreated, rel)
}

// PATCH /api/v1/relationships/{id}
func (h *Handler) UpdateRelationship(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		handleErr(w, r, err)
		return
	}
	var req RelationshipRequest
	if err := decodeJSON(r, &req); err != nil {
		handleErr(w, r, err)
		return
	}
	existing, err := h.Store.GetRelationship(r.Context(), id)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	if req.FromPersonID != "" {
		existing.FromPersonID = req.FromPersonID
	}
	if req.ToPersonID != "" {
		existing.ToPersonID = req.ToPersonID
	}
	if req.Type != "" {
		existing.Type = domain.RelationshipType(req.Type)
	}
	if req.Confidence != "" {
		existing.Confidence = domain.Confidence(req.Confidence)
	}
	if req.Note != "" || req.Note == "" {
		existing.Note = req.Note
	}
	if err := h.Store.UpdateRelationship(r.Context(), &existing); err != nil {
		handleErr(w, r, err)
		return
	}
	if got, err := h.Store.GetRelationship(r.Context(), existing.ID); err == nil {
		existing = got
	}
	httputil.WriteJSON(w, r, http.StatusOK, existing)
}

// DELETE /api/v1/relationships/{id}
func (h *Handler) DeleteRelationship(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		handleErr(w, r, err)
		return
	}
	if err := h.Store.DeleteRelationship(r.Context(), id); err != nil {
		handleErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
