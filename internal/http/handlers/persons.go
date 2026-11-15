package handlers

import (
	"context"
	"net/http"

	httputil "memorybutton/backend/internal/httputil"
	"memorybutton/backend/internal/domain"
	"memorybutton/backend/internal/storage/postgres"
)

// GET /api/v1/persons
func (h *Handler) ListPersons(w http.ResponseWriter, r *http.Request) {
	persons, total, err := h.Store.ListPersons(r.Context(), postgres.PersonFilter{
		Search: r.URL.Query().Get("search"),
		Limit:  queryInt(r, "limit", 50),
		Offset: queryInt(r, "offset", 0),
	})
	if err != nil {
		handleErr(w, r, err)
		return
	}
	out := make([]any, 0, len(persons))
	for _, p := range persons {
		out = append(out, h.personWithURL(r.Context(), p))
	}
	httputil.WriteJSON(w, r, http.StatusOK, map[string]any{
		"persons": out, "total": total,
		"limit": queryInt(r, "limit", 50), "offset": queryInt(r, "offset", 0),
	})
}

// POST /api/v1/persons
func (h *Handler) CreatePerson(w http.ResponseWriter, r *http.Request) {
	var req PersonRequest
	if err := decodeJSON(r, &req); err != nil {
		handleErr(w, r, err)
		return
	}
	if trimLower(req.DisplayName) == "" {
		handleErr(w, r, badRequest("display_name обязателен"))
		return
	}
	p := req.toDomain()
	p.NameVariants = normalizeSlice(p.NameVariants)
	p.Places = normalizeSlice(p.Places)
	if err := h.Store.CreatePerson(r.Context(), &p); err != nil {
		handleErr(w, r, err)
		return
	}
	httputil.WriteJSON(w, r, http.StatusCreated, h.personWithURL(r.Context(), p))
}

// GET /api/v1/persons/{id}
func (h *Handler) GetPerson(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		handleErr(w, r, err)
		return
	}
	card, err := h.Store.PersonCard(r.Context(), id)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	card.Person = h.personWithURL(r.Context(), card.Person)
	httputil.WriteJSON(w, r, http.StatusOK, card)
}

// PATCH /api/v1/persons/{id}
func (h *Handler) UpdatePerson(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		handleErr(w, r, err)
		return
	}
	var req PersonRequest
	if err := decodeJSON(r, &req); err != nil {
		handleErr(w, r, err)
		return
	}
	if trimLower(req.DisplayName) == "" {
		handleErr(w, r, badRequest("display_name обязателен"))
		return
	}
	existing, err := h.Store.GetPerson(r.Context(), id)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	p := req.toDomain()
	p.ID = existing.ID
	p.NameVariants = normalizeSlice(p.NameVariants)
	p.Places = normalizeSlice(p.Places)
	if err := h.Store.UpdatePerson(r.Context(), &p); err != nil {
		handleErr(w, r, err)
		return
	}
	httputil.WriteJSON(w, r, http.StatusOK, h.personWithURL(r.Context(), p))
}

// POST /api/v1/persons/{id}/photo (multipart, поле "file")
func (h *Handler) UploadPersonPhoto(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		handleErr(w, r, err)
		return
	}
	if _, err := h.Store.GetPerson(r.Context(), id); err != nil {
		handleErr(w, r, err)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		handleErr(w, r, badRequest("нужен файл в поле file"))
		return
	}
	defer file.Close()
	if header.Size > 32<<20 {
		handleErr(w, r, &httputil.HTTPError{Status: http.StatusRequestEntityTooLarge,
			Code: httputil.CodeTooLarge, Msg: "файл больше 32 МБ"})
		return
	}
	m, err := h.Media.UploadReader(r.Context(), domain.MediaPhoto, "person", id, header.Filename, file, header.Size, header.Header.Get("Content-Type"))
	if err != nil {
		handleErr(w, r, err)
		return
	}
	if err := h.Store.SetPersonPhoto(r.Context(), id, m.ID); err != nil {
		handleErr(w, r, err)
		return
	}
	httputil.WriteJSON(w, r, http.StatusCreated, m)
}

func (h *Handler) personWithURL(ctx context.Context, p domain.Person) domain.Person {
	if p.PhotoMediaID != nil {
		if m, err := h.Store.GetMedia(ctx, *p.PhotoMediaID); err == nil {
			p.PhotoURL = m.URL
		}
	}
	return p
}
