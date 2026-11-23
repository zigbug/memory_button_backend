// Package handlers содержит HTTP-обработчики Memory Button API.
package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"memorybutton/backend/internal/ai"
	"memorybutton/backend/internal/config"
	httputil "memorybutton/backend/internal/httputil"
	"memorybutton/backend/internal/pipeline"
	"memorybutton/backend/internal/storage/media"
	"memorybutton/backend/internal/storage/postgres"
	"memorybutton/backend/internal/storage/s3"
)

type Handler struct {
	Store    *postgres.Store
	AI       ai.Provider
	Media    *media.Service
	Storage  s3.Storage
	Dialogue *pipeline.Dialogue
	Post     *pipeline.Postprocessor
	QA       *pipeline.QAService
	Swagger  *SwaggerHandler
	Config   *config.Config
	Logger   *slog.Logger
}

func New(store *postgres.Store, provider ai.Provider, mediaSvc *media.Service, storage s3.Storage,
	dialogue *pipeline.Dialogue, post *pipeline.Postprocessor, qa *pipeline.QAService,
	swagger *SwaggerHandler, cfg *config.Config, logger *slog.Logger) *Handler {
	return &Handler{
		Store: store, AI: provider, Media: mediaSvc, Storage: storage,
		Dialogue: dialogue, Post: post, QA: qa, Swagger: swagger, Config: cfg, Logger: logger,
	}
}

// ---------- helpers ----------

func decodeJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return httputil.ErrBadRequest
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 4<<20))
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return httputil.ErrBadRequest
		}
		return fmt.Errorf("%w: %v", httputil.ErrBadRequest, err)
	}
	return nil
}

func queryInt(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	return n
}

func pathUUID(r *http.Request, key string) (string, error) {
	v := r.PathValue(key)
	if _, err := uuid.Parse(v); err != nil {
		return "", fmt.Errorf("%w: %s должен быть UUID", httputil.ErrBadRequest, key)
	}
	return v, nil
}

func validUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}

func handleErr(w http.ResponseWriter, r *http.Request, err error) {
	// Логируем неожиданные ошибки (5xx) с request_id для диагностики.
	var he *httputil.HTTPError
	if !errors.As(err, &he) && !isKnownAppError(err) {
		reqID := ""
		if r != nil {
			reqID = httputil.RequestIDFrom(r.Context())
		}
		slog.Default().Error("http handler error",
			"err", err,
			"request_id", reqID,
			"method", r.Method,
			"path", r.URL.Path,
		)
	}
	httputil.WriteHTTPErr(w, r, err)
}

func isKnownAppError(err error) bool {
	switch {
	case errors.Is(err, postgres.ErrNotFound),
		errors.Is(err, postgres.ErrDraftAlreadyConfirmed),
		errors.Is(err, postgres.ErrDraftDismissed),
		errors.Is(err, postgres.ErrSessionNotActive):
		return true
	default:
		return false
	}
}

func badRequest(msg string) error {
	return &httputil.HTTPError{Status: http.StatusBadRequest, Code: httputil.CodeValidation, Msg: msg}
}

func normalizeSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func trimLower(s string) string { return strings.TrimSpace(s) }
