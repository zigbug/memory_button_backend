// Package middleware — request id, логирование, recovery и авторизация.
package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"memorybutton/backend/internal/auth"
	"memorybutton/backend/internal/config"
	httputil "memorybutton/backend/internal/httputil"
	"memorybutton/backend/internal/storage/postgres"
)

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(httputil.WithRequestID(r.Context(), id)))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

func Logger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r)
			if sw.status == 0 {
				sw.status = http.StatusOK
			}
			logger.Info("http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"bytes", sw.bytes,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", httputil.RequestIDFrom(r.Context()),
			)
		})
	}
}

func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic", "err", rec, "path", r.URL.Path,
						"request_id", httputil.RequestIDFrom(r.Context()))
					httputil.WriteError(w, r, http.StatusInternalServerError, "internal", "внутренняя ошибка сервера")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Auth проверяет JWT (HS256) или статический AUTH_STATIC_TOKEN.
func Auth(cfg config.JWTConfig, staticToken, username string, store *postgres.Store, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := bearer(r)
			if tokenStr == "" {
				httputil.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "требуется авторизация")
				return
			}
			if staticToken != "" && tokenStr == staticToken {
				u, _, err := store.GetUserByUsername(r.Context(), username)
				if err != nil {
					logger.Error("статический токен: пользователь не найден", "username", username, "err", err)
					httputil.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "пользователь не найден")
					return
				}
				claims := &auth.Claims{UserID: u.ID, Username: u.Username}
				next.ServeHTTP(w, r.WithContext(httputil.WithClaims(r.Context(), claims)))
				return
			}
			claims, err := auth.ParseToken(cfg, tokenStr)
			if err != nil {
				httputil.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "невалидный или истёкший токен")
				return
			}
			next.ServeHTTP(w, r.WithContext(httputil.WithClaims(r.Context(), claims)))
		})
	}
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && (h[:7] == "Bearer " || h[:7] == "bearer ") {
		return h[7:]
	}
	return ""
}

