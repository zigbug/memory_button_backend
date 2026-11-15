package handlers

import (
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	httputil "memorybutton/backend/internal/httputil"
	"memorybutton/backend/internal/domain"
)

// GET /api/v1/media/{id} — отдаёт метаданные и актуальный URL.
func (h *Handler) GetMedia(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		handleErr(w, r, err)
		return
	}
	m, err := h.Store.GetMedia(r.Context(), id)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	if u, err := h.Storage.URL(r.Context(), m.StorageKey); err == nil {
		m.URL = u
	}
	httputil.WriteJSON(w, r, http.StatusOK, m)
}

// GET /api/v1/media/{id}/content — проксирует байты (удобно, если бакет приватный).
func (h *Handler) GetMediaContent(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		handleErr(w, r, err)
		return
	}
	m, rc, err := h.Media.Get(r.Context(), id)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	defer rc.Close()
	if m.MIME != "" {
		w.Header().Set("Content-Type", m.MIME)
	}
	_, _ = io.Copy(w, rc)
}

// POST /api/v1/media/upload — универсальная multipart-загрузка медиа.
func (h *Handler) UploadMedia(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		handleErr(w, r, badRequest("не удалось разобрать multipart"))
		return
	}
	kind := domain.MediaKind(r.FormValue("kind"))
	if kind == "" {
		kind = domain.MediaOther
	}
	ownerKind := r.FormValue("owner_kind")
	ownerID := r.FormValue("owner_id")
	file, header, err := r.FormFile("file")
	if err != nil {
		handleErr(w, r, badRequest("нужен файл в поле file"))
		return
	}
	defer file.Close()
	m, err := h.Media.UploadReader(r.Context(), kind, ownerKind, ownerID, header.Filename, file, header.Size, header.Header.Get("Content-Type"))
	if err != nil {
		handleErr(w, r, err)
		return
	}
	httputil.WriteJSON(w, r, http.StatusCreated, m)
}

// POST /api/v1/media/presign — выдаёт presigned PUT (альтернативный режим загрузки).
func (h *Handler) PresignUpload(w http.ResponseWriter, r *http.Request) {
	var req PresignRequest
	if err := decodeJSON(r, &req); err != nil {
		handleErr(w, r, err)
		return
	}
	if req.Kind == "" {
		req.Kind = "other"
	}
	if req.OwnerKind == "" {
		req.OwnerKind = "generic"
	}
	if req.OwnerID == "" {
		req.OwnerID = "none"
	}
	key := req.Kind + "/" + req.OwnerKind + "/" + req.OwnerID + "/" + uuid.NewString() + "-" + req.Filename
	url, err := h.Storage.PresignPut(r.Context(), key, 15*time.Minute)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	httputil.WriteJSON(w, r, http.StatusOK, PresignResponse{
		StorageKey: key, UploadURL: url, Method: "PUT",
		Headers: map[string]string{"Content-Type": req.MIME},
	})
}
