package httpapi

import (
	"net/http"

	"memorybutton/backend/internal/auth"
	"memorybutton/backend/internal/config"
	"memorybutton/backend/internal/http/handlers"
	"memorybutton/backend/internal/http/middleware"
	"memorybutton/backend/internal/storage/postgres"
	"log/slog"
)

func NewRouter(h *handlers.Handler, cfg *config.Config, store *postgres.Store, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	// Публичные.
	mux.HandleFunc("GET /health", h.Health)
	mux.HandleFunc("GET /api/v1/health", h.Health)
	mux.HandleFunc("POST /api/v1/auth/login", h.Login)

	// Защищённые.
	authed := http.NewServeMux()
	authed.HandleFunc("GET /api/v1/me", h.Me)

	// Persons
	authed.HandleFunc("GET /api/v1/persons", h.ListPersons)
	authed.HandleFunc("POST /api/v1/persons", h.CreatePerson)
	authed.HandleFunc("GET /api/v1/persons/{id}", h.GetPerson)
	authed.HandleFunc("PATCH /api/v1/persons/{id}", h.UpdatePerson)
	authed.HandleFunc("POST /api/v1/persons/{id}/photo", h.UploadPersonPhoto)
	authed.HandleFunc("GET /api/v1/persons/{id}/stories", h.ListPersonStories)
	authed.HandleFunc("GET /api/v1/persons/{id}/questions", h.ListPersonQuestions)

	// Relationships
	authed.HandleFunc("GET /api/v1/relationships", h.ListRelationships)
	authed.HandleFunc("POST /api/v1/relationships", h.CreateRelationship)
	authed.HandleFunc("PATCH /api/v1/relationships/{id}", h.UpdateRelationship)
	authed.HandleFunc("DELETE /api/v1/relationships/{id}", h.DeleteRelationship)

	// Sessions (turn-by-turn)
	authed.HandleFunc("POST /api/v1/sessions", h.CreateSession)
	authed.HandleFunc("GET /api/v1/sessions/{id}", h.GetSession)
	authed.HandleFunc("POST /api/v1/sessions/{id}/turns", h.CreateTurn)
	authed.HandleFunc("POST /api/v1/sessions/{id}/complete", h.CompleteSession)
	authed.HandleFunc("GET /api/v1/sessions/{id}/draft", h.GetSessionDraft)

	// Drafts / stories
	authed.HandleFunc("POST /api/v1/drafts/{session_id}/confirm", h.ConfirmDraft)
	authed.HandleFunc("GET /api/v1/stories/{id}", h.GetStory)
	authed.HandleFunc("POST /api/v1/stories/{id}/confirm", h.ConfirmStory)

	// Questions
	authed.HandleFunc("GET /api/v1/questions", h.ListQuestions)
	authed.HandleFunc("POST /api/v1/questions", h.CreateQuestion)
	authed.HandleFunc("PATCH /api/v1/questions/{id}", h.UpdateQuestion)

	// Q&A
	authed.HandleFunc("POST /api/v1/qa/ask", h.AskQA)

	// Media
	authed.HandleFunc("GET /api/v1/media/{id}", h.GetMedia)
	authed.HandleFunc("GET /api/v1/media/{id}/content", h.GetMediaContent)
	authed.HandleFunc("POST /api/v1/media/upload", h.UploadMedia)
	authed.HandleFunc("POST /api/v1/media/presign", h.PresignUpload)

	authMW := middleware.Auth(cfg.JWT, cfg.Auth.StaticToken, cfg.Auth.Username, store, logger)
	mux.Handle("/api/v1/", authMW(authed))

	// Глобальные middleware.
	var handler http.Handler = mux
	handler = middleware.Logger(logger)(handler)
	handler = middleware.RequestID(handler)
	handler = middleware.Recovery(logger)(handler)
	return handler
}

var _ = auth.Claims{}
