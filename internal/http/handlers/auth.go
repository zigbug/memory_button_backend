package handlers

import (
	"net/http"

	"memorybutton/backend/internal/auth"
	httputil "memorybutton/backend/internal/httputil"
)

// POST /api/v1/auth/login
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := decodeJSON(r, &req); err != nil {
		handleErr(w, r, err)
		return
	}
	if req.Username == "" || req.Password == "" {
		handleErr(w, r, badRequest("username и password обязательны"))
		return
	}
	u, hash, err := h.Store.GetUserByUsername(r.Context(), req.Username)
	if err != nil || !auth.CheckPassword(hash, req.Password) {
		httputil.WriteError(w, r, http.StatusUnauthorized, "invalid_credentials", "неверный логин или пароль")
		return
	}
	token, err := auth.GenerateToken(h.Config.JWT, u.ID, u.Username)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	httputil.WriteJSON(w, r, http.StatusOK, LoginResponse{
		Token:     token,
		TokenType: "Bearer",
		ExpiresIn: int64(h.Config.JWT.TTL.Seconds()),
		User:      u,
	})
}

// GET /api/v1/me
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	claims := httputil.ClaimsFrom(r.Context())
	if claims == nil {
		httputil.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "требуется авторизация")
		return
	}
	u, _, err := h.Store.GetUserByUsername(r.Context(), claims.Username)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	httputil.WriteJSON(w, r, http.StatusOK, u)
}
