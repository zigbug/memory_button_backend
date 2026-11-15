package handlers

import (
	"io"
	"net/http"

	httputil "memorybutton/backend/internal/httputil"
	"memorybutton/backend/internal/domain"
	"memorybutton/backend/internal/pipeline"
	"memorybutton/backend/internal/storage/postgres"
)

const maxAudioUpload = 32 << 20

// POST /api/v1/sessions
func (h *Handler) CreateSession(w http.ResponseWriter, r *http.Request) {
	var req CreateSessionRequest
	if err := decodeJSON(r, &req); err != nil {
		handleErr(w, r, err)
		return
	}
	mode := domain.SessionMode(req.Mode)
	personID := ""
	if req.PersonID != nil {
		personID = *req.PersonID
	}
	sess, greeting, err := h.Dialogue.StartSession(r.Context(), mode, personID, req.QuestionIDs)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	// Обогащаем ответ контекстом (что известно, предлагаемые вопросы).
	resp := map[string]any{
		"session":     sess,
		"greeting":    greeting,
		"known_facts": []domain.Fact{},
		"open_questions": []domain.Question{},
	}
	if personID != "" {
		if facts, err := h.Store.ListFacts(r.Context(), domain.FactFilter{PersonID: personID, ConfirmedOnly: true, Limit: 20}); err == nil {
			resp["known_facts"] = facts
		}
		if qs, err := h.Store.OpenQuestions(r.Context(), personID, 10); err == nil {
			resp["open_questions"] = qs
		}
	}
	httputil.WriteJSON(w, r, http.StatusCreated, resp)
}

// GET /api/v1/sessions/{id}
func (h *Handler) GetSession(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		handleErr(w, r, err)
		return
	}
	sess, err := h.Store.GetSession(r.Context(), id)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	turns, err := h.Store.ListTurns(r.Context(), id)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	// Подставим URL аудио.
	for i := range turns {
		if turns[i].AudioMediaID != nil {
			if m, err := h.Store.GetMedia(r.Context(), *turns[i].AudioMediaID); err == nil {
				turns[i].AudioURL = m.URL
			}
		}
	}
	httputil.WriteJSON(w, r, http.StatusOK, map[string]any{
		"session": sess,
		"turns":   turns,
	})
}

// POST /api/v1/sessions/{id}/turns (multipart audio)
func (h *Handler) CreateTurn(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		handleErr(w, r, err)
		return
	}
	if err := r.ParseMultipartForm(maxAudioUpload); err != nil {
		handleErr(w, r, badRequest("не удалось разобрать multipart: "+err.Error()))
		return
	}
	file, header, err := r.FormFile("audio")
	if err != nil {
		// Допускаем поле "file" для удобства.
		file, header, err = r.FormFile("file")
	}
	if err != nil {
		handleErr(w, r, badRequest("нужен аудиофайл в поле audio"))
		return
	}
	defer file.Close()
	if header.Size > maxAudioUpload {
		handleErr(w, r, &httputil.HTTPError{Status: http.StatusRequestEntityTooLarge, Code: httputil.CodeTooLarge, Msg: "аудио больше 32 МБ"})
		return
	}
	audio, err := io.ReadAll(io.LimitReader(file, maxAudioUpload+1))
	if err != nil {
		handleErr(w, r, err)
		return
	}
	res, err := h.Dialogue.Turn(r.Context(), id, audio, header.Filename)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	httputil.WriteJSON(w, r, http.StatusOK, map[string]any{
		"recognized_text": res.UserTurn.Text,
		"reply":           res.AssistantTurn,
		"user_turn":       res.UserTurn,
		"status":          "ok",
	})
}

// POST /api/v1/sessions/{id}/complete
func (h *Handler) CompleteSession(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		handleErr(w, r, err)
		return
	}
	if err := h.Dialogue.Complete(r.Context(), id, h.Config.Worker.SyncProcessing, h.Post); err != nil {
		handleErr(w, r, err)
		return
	}
	httputil.WriteJSON(w, r, http.StatusAccepted, map[string]any{
		"session_id": id, "status": "completed",
		"processing": map[string]bool{"sync": h.Config.Worker.SyncProcessing},
	})
}

// GET /api/v1/sessions/{id}/draft
func (h *Handler) GetSessionDraft(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		handleErr(w, r, err)
		return
	}
	draft, err := h.Store.GetDraftBySession(r.Context(), id)
	if err != nil {
		if err == postgres.ErrNotFound {
			httputil.WriteJSON(w, r, http.StatusOK, map[string]any{
				"status": "pending", "message": "черновик ещё не готов",
			})
			return
		}
		handleErr(w, r, err)
		return
	}
	httputil.WriteJSON(w, r, http.StatusOK, draft)
}

// POST /api/v1/drafts/{session_id}/confirm
func (h *Handler) ConfirmDraft(w http.ResponseWriter, r *http.Request) {
	sessionID, err := pathUUID(r, "session_id")
	if err != nil {
		handleErr(w, r, err)
		return
	}
	var req ConfirmDraftRequest
	if err := decodeJSON(r, &req); err != nil {
		handleErr(w, r, err)
		return
	}
	// Валидация действий.
	for _, d := range req.Items {
		if !validUUID(d.ItemID) {
			handleErr(w, r, badRequest("item_id должен быть UUID"))
			return
		}
		if d.Action != "confirm" && d.Action != "dismiss" {
			handleErr(w, r, badRequest("action: confirm|dismiss"))
			return
		}
	}
	err = h.Store.ConfirmDraft(r.Context(), sessionID, req.Items, req.StoryText, req.StoryPersonIDs)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	draft, err := h.Store.GetDraftBySession(r.Context(), sessionID)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	httputil.WriteJSON(w, r, http.StatusOK, map[string]any{
		"status": "confirmed", "draft": draft,
	})
}

var _ = pipeline.JobPostprocess
