package httputil

import (
	"encoding/json"
	"errors"
	"net/http"

	"memorybutton/backend/internal/storage/postgres"
)

type ErrorResponse struct {
	Error     string `json:"error"`
	Code      string `json:"code,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

func WriteJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Поздно что-то менять — клиент получит обрезанный ответ.
		return
	}
}

func WriteError(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	resp := ErrorResponse{Error: msg, Code: code}
	if r != nil {
		resp.RequestID = RequestIDFrom(r.Context())
	}
	WriteJSON(w, r, status, resp)
}

// HTTPError — ошибка с маппингом на статус.
type HTTPError struct {
	Status int
	Code   string
	Msg    string
}

func (e *HTTPError) Error() string { return e.Msg }

func newHTTPError(status int, code, msg string) *HTTPError {
	return &HTTPError{Status: status, Code: code, Msg: msg}
}

var (
	ErrBadRequest = newHTTPError(http.StatusBadRequest, "invalid_request", "некорректный запрос")
	ErrUnauthorized = newHTTPError(http.StatusUnauthorized, "unauthorized", "требуется авторизация")
	ErrNotFound = newHTTPError(http.StatusNotFound, "not_found", "не найдено")
	ErrConflict = newHTTPError(http.StatusConflict, "conflict", "конфликт состояния")
)

func WriteHTTPErr(w http.ResponseWriter, r *http.Request, err error) {
	var he *HTTPError
	if errors.As(err, &he) {
		WriteError(w, r, he.Status, he.Code, he.Msg)
		return
	}
	switch {
	case errors.Is(err, postgres.ErrNotFound):
		WriteError(w, r, http.StatusNotFound, "not_found", "не найдено")
	case errors.Is(err, postgres.ErrDraftAlreadyConfirmed):
		WriteError(w, r, http.StatusConflict, "draft_confirmed", "черновик уже подтверждён")
	case errors.Is(err, postgres.ErrDraftDismissed):
		WriteError(w, r, http.StatusConflict, "draft_dismissed", "черновик отклонён")
	case errors.Is(err, postgres.ErrSessionNotActive):
		WriteError(w, r, http.StatusConflict, "session_not_active", "сессия завершена")
	default:
		WriteError(w, r, http.StatusInternalServerError, "internal", "внутренняя ошибка сервера")
	}
}

// Коды ошибок, возвращаемые клиенту.
const (
	CodeValidation     = "validation_error"
	CodeInvalidJSON    = "invalid_json"
	CodeTooLarge       = "too_large"
	CodeUnsupported    = "unsupported_media_type"
)
