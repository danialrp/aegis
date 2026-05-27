// SPDX-License-Identifier: AGPL-3.0-or-later

// Package v1 contains the controller's first-version HTTP API
// handlers. Routes are registered via Mount.
package v1

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/danialrp/aegis/internal/api/middleware"
	"github.com/danialrp/aegis/internal/auth"
)

// AuthHandler serves the /v1/auth/* endpoints.
type AuthHandler struct {
	svc    *auth.Service
	logger *slog.Logger
}

// NewAuthHandler builds an AuthHandler over the given service.
func NewAuthHandler(svc *auth.Service, logger *slog.Logger) *AuthHandler {
	return &AuthHandler{svc: svc, logger: logger}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"expires_at"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// Login handles POST /v1/auth/login.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	ip := clientIP(r)
	result, _, err := h.svc.Login(r.Context(), req.Email, req.Password, ip, r.UserAgent())
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	case errors.Is(err, auth.ErrAccountDisabled):
		writeError(w, http.StatusForbidden, "account_disabled")
		return
	case err != nil:
		h.logger.ErrorContext(r.Context(), "login internal error", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	writeJSON(w, http.StatusOK, tokenResponse{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    result.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

// Refresh handles POST /v1/auth/refresh.
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	ip := clientIP(r)
	result, err := h.svc.Refresh(r.Context(), req.RefreshToken, ip, r.UserAgent())
	switch {
	case errors.Is(err, auth.ErrInvalidSession):
		writeError(w, http.StatusUnauthorized, "invalid_refresh_token")
		return
	case errors.Is(err, auth.ErrAccountDisabled):
		writeError(w, http.StatusForbidden, "account_disabled")
		return
	case err != nil:
		h.logger.ErrorContext(r.Context(), "refresh internal error", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	writeJSON(w, http.StatusOK, tokenResponse{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    result.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

// Logout handles POST /v1/auth/logout. Requires authentication.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	sessionID, _ := middleware.SessionIDFromContext(r.Context())
	user, _ := middleware.UserFromContext(r.Context())
	ip := clientIP(r)

	if err := h.svc.Logout(r.Context(), sessionID, user.ID, ip); err != nil {
		h.logger.ErrorContext(r.Context(), "logout failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Me handles GET /v1/auth/me. Requires authentication.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	user, _ := middleware.UserFromContext(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      user.ID,
		"email":   user.Email,
		"role":    user.Role,
		"enabled": user.Enabled,
	})
}

// --- helpers ---

func clientIP(r *http.Request) *netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return nil
	}
	return &addr
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}
