// SPDX-License-Identifier: AGPL-3.0-or-later

package v1

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/danialrp/aegis/internal/api/middleware"
	"github.com/danialrp/aegis/internal/audit"
	"github.com/danialrp/aegis/internal/auth"
	"github.com/danialrp/aegis/internal/db/sqlc"
)

// UsersHandler serves /v1/users/*.
//
// All routes here require god or admin role; the existing
// RequireRole middleware in v1.Mount handles that gate. A user
// cannot modify themselves into a god (or out of one) — role changes
// are gated to god callers explicitly inside Update.
type UsersHandler struct {
	queries  *sqlc.Queries
	auditRec *audit.Recorder
	logger   *slog.Logger
}

// NewUsersHandler builds the handler.
func NewUsersHandler(q *sqlc.Queries, a *audit.Recorder, logger *slog.Logger) *UsersHandler {
	return &UsersHandler{queries: q, auditRec: a, logger: logger}
}

// --- shapes ---

type createUserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
	Enabled  bool   `json:"enabled"`
}

type updateUserRequest struct {
	// Role + Enabled are optional. Password rotates if non-empty.
	Role     string `json:"role,omitempty"`
	Enabled  *bool  `json:"enabled,omitempty"`
	Password string `json:"password,omitempty"`
}

type userResponse struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
}

// --- handlers ---

// List handles GET /v1/users.
func (h *UsersHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.queries.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	out := make([]userResponse, 0, len(rows))
	for _, u := range rows {
		out = append(out, userToResponse(u))
	}
	writeJSON(w, http.StatusOK, out)
}

// Get handles GET /v1/users/{id}.
func (h *UsersHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}
	u, err := h.queries.GetUserByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, userToResponse(u))
}

// Create handles POST /v1/users. Only god can create new god/admin
// users; admin can only create site_users.
func (h *UsersHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if !strings.Contains(req.Email, "@") {
		writeError(w, http.StatusBadRequest, "invalid_email")
		return
	}
	if !auth.Role(req.Role).Valid() {
		writeError(w, http.StatusBadRequest, "invalid_role")
		return
	}
	caller, _ := middleware.UserFromContext(r.Context())
	if caller == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	// Admin can't escalate to god/admin.
	if caller.Role != string(auth.RoleGod) && req.Role != string(auth.RoleSiteUser) {
		writeError(w, http.StatusForbidden, "role_requires_god")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, "password_too_short")
		return
	}
	row, err := h.queries.CreateUser(r.Context(), sqlc.CreateUserParams{
		Email: req.Email, PasswordHash: hash,
		Role: req.Role, Enabled: req.Enabled,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "email_already_used")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	h.recordAudit(r.Context(), r, row.ID, "user.created",
		map[string]any{"email": req.Email, "role": req.Role})
	writeJSON(w, http.StatusCreated, userToResponse(row))
}

// Update handles PATCH /v1/users/{id}.
func (h *UsersHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}
	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	caller, _ := middleware.UserFromContext(r.Context())
	if caller == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	target, err := h.queries.GetUserByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}

	if req.Role != "" {
		if !auth.Role(req.Role).Valid() {
			writeError(w, http.StatusBadRequest, "invalid_role")
			return
		}
		if caller.Role != string(auth.RoleGod) {
			writeError(w, http.StatusForbidden, "role_requires_god")
			return
		}
		// Role change is structural — disallow via this PATCH for now;
		// dedicated endpoint when we add an audit + reason flow.
		writeError(w, http.StatusBadRequest, "role_changes_not_supported")
		return
	}
	if req.Enabled != nil {
		// Admin can disable a site_user; only god can disable an admin.
		if target.Role == string(auth.RoleAdmin) && caller.Role != string(auth.RoleGod) {
			writeError(w, http.StatusForbidden, "requires_god")
			return
		}
		if target.Role == string(auth.RoleGod) {
			writeError(w, http.StatusForbidden, "cannot_modify_god")
			return
		}
		if err := h.queries.SetUserEnabled(r.Context(), sqlc.SetUserEnabledParams{
			ID: id, Enabled: *req.Enabled,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	if req.Password != "" {
		hash, herr := auth.HashPassword(req.Password)
		if herr != nil {
			writeError(w, http.StatusBadRequest, "password_too_short")
			return
		}
		if err := h.queries.UpdateUserPassword(r.Context(), sqlc.UpdateUserPasswordParams{
			ID: id, PasswordHash: hash,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
	}

	h.recordAudit(r.Context(), r, id, "user.updated", nil)
	updated, _ := h.queries.GetUserByID(r.Context(), id)
	writeJSON(w, http.StatusOK, userToResponse(updated))
}

// Delete handles DELETE /v1/users/{id}.
func (h *UsersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}
	caller, _ := middleware.UserFromContext(r.Context())
	if caller == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if caller.ID == id {
		writeError(w, http.StatusBadRequest, "cannot_delete_self")
		return
	}
	target, err := h.queries.GetUserByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if target.Role == string(auth.RoleGod) {
		writeError(w, http.StatusForbidden, "cannot_delete_god")
		return
	}
	if err := h.queries.DeleteUser(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	h.recordAudit(r.Context(), r, id, "user.deleted", map[string]any{"email": target.Email})
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func userToResponse(u sqlc.User) userResponse {
	return userResponse{
		ID:        u.ID,
		Email:     u.Email,
		Role:      u.Role,
		Enabled:   u.Enabled,
		CreatedAt: tsString(u.CreatedAt),
	}
}

func (h *UsersHandler) recordAudit(ctx context.Context, r *http.Request, targetID int64, action string, payload any) {
	id := targetID
	user, _ := middleware.UserFromContext(ctx)
	var actorID *int64
	if user != nil {
		actorID = &user.ID
	}
	ip := clientIP(r)
	if err := h.auditRec.Record(ctx, audit.Event{
		ActorUserID: actorID, ActorIP: ip,
		Action: action, TargetType: "user",
		TargetID: &id, Payload: payload,
	}); err != nil {
		h.logger.WarnContext(ctx, "audit record failed", "action", action, "err", err)
	}
}
