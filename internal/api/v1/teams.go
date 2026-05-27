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
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/danialrp/aegis/internal/api/middleware"
	"github.com/danialrp/aegis/internal/audit"
	"github.com/danialrp/aegis/internal/db/sqlc"
)

// TeamsHandler serves /v1/teams/*.
type TeamsHandler struct {
	queries  *sqlc.Queries
	auditRec *audit.Recorder
	logger   *slog.Logger
}

// NewTeamsHandler builds the handler.
func NewTeamsHandler(q *sqlc.Queries, a *audit.Recorder, logger *slog.Logger) *TeamsHandler {
	return &TeamsHandler{queries: q, auditRec: a, logger: logger}
}

// --- shapes ---

type teamRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type teamResponse struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

type teamMemberRequest struct {
	UserID int64  `json:"user_id"`
	Role   string `json:"role,omitempty"` // owner | member; defaults to member
}

type teamMemberResponse struct {
	TeamID  int64  `json:"team_id"`
	UserID  int64  `json:"user_id"`
	Email   string `json:"email"`
	Role    string `json:"role_in_team"`
	UserRol string `json:"user_role"`
	AddedAt string `json:"added_at"`
}

// --- handlers ---

// List handles GET /v1/teams.
func (h *TeamsHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.queries.ListTeams(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	out := make([]teamResponse, 0, len(rows))
	for _, t := range rows {
		out = append(out, teamToResponse(t))
	}
	writeJSON(w, http.StatusOK, out)
}

// Get handles GET /v1/teams/{id}.
func (h *TeamsHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}
	t, err := h.queries.GetTeam(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, teamToResponse(t))
}

// Create handles POST /v1/teams.
func (h *TeamsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req teamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name_required")
		return
	}
	t, err := h.queries.CreateTeam(r.Context(), sqlc.CreateTeamParams{
		Name:        req.Name,
		Description: pgText(req.Description),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	h.recordAudit(r.Context(), r, t.ID, "team.created", map[string]any{"name": req.Name})
	writeJSON(w, http.StatusCreated, teamToResponse(t))
}

// Update handles PATCH /v1/teams/{id}.
func (h *TeamsHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}
	var req teamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name_required")
		return
	}
	t, err := h.queries.UpdateTeam(r.Context(), sqlc.UpdateTeamParams{
		ID: id, Name: req.Name, Description: pgText(req.Description),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	h.recordAudit(r.Context(), r, id, "team.updated", nil)
	writeJSON(w, http.StatusOK, teamToResponse(t))
}

// Delete handles DELETE /v1/teams/{id}.
func (h *TeamsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}
	if err := h.queries.DeleteTeam(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	h.recordAudit(r.Context(), r, id, "team.deleted", nil)
	w.WriteHeader(http.StatusNoContent)
}

// ListMembers handles GET /v1/teams/{id}/members.
func (h *TeamsHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}
	rows, err := h.queries.ListTeamMembers(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	out := make([]teamMemberResponse, 0, len(rows))
	for _, m := range rows {
		out = append(out, teamMemberResponse{
			TeamID:  m.TeamID,
			UserID:  m.UserID,
			Email:   m.Email,
			Role:    m.RoleInTeam,
			UserRol: m.Role,
			AddedAt: tsString(m.AddedAt),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// AddMember handles POST /v1/teams/{id}/members.
func (h *TeamsHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}
	var req teamMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if req.Role == "" {
		req.Role = "member"
	}
	if req.Role != "owner" && req.Role != "member" {
		writeError(w, http.StatusBadRequest, "invalid_role_in_team")
		return
	}
	if _, err := h.queries.GetUserByID(r.Context(), req.UserID); err != nil {
		writeError(w, http.StatusBadRequest, "user_not_found")
		return
	}
	if err := h.queries.AddTeamMember(r.Context(), sqlc.AddTeamMemberParams{
		TeamID: id, UserID: req.UserID, RoleInTeam: req.Role,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	h.recordAudit(r.Context(), r, id, "team.member.added",
		map[string]any{"user_id": req.UserID, "role": req.Role})
	w.WriteHeader(http.StatusNoContent)
}

// RemoveMember handles DELETE /v1/teams/{id}/members/{user_id}.
func (h *TeamsHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}
	uid, err := strconv.ParseInt(chi.URLParam(r, "user_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_user_id")
		return
	}
	if err := h.queries.RemoveTeamMember(r.Context(), sqlc.RemoveTeamMemberParams{
		TeamID: id, UserID: uid,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	h.recordAudit(r.Context(), r, id, "team.member.removed", map[string]any{"user_id": uid})
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func teamToResponse(t sqlc.Team) teamResponse {
	r := teamResponse{
		ID:        t.ID,
		Name:      t.Name,
		CreatedAt: tsString(t.CreatedAt),
	}
	if t.Description.Valid {
		v := t.Description.String
		r.Description = &v
	}
	return r
}

func pgText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func pgInt8(v int64) pgtype.Int8 {
	return pgtype.Int8{Int64: v, Valid: true}
}

func parseInt64Param(w http.ResponseWriter, r *http.Request, key string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, key), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return 0, false
	}
	return id, true
}

func (h *TeamsHandler) recordAudit(ctx context.Context, r *http.Request, teamID int64, action string, payload any) {
	id := teamID
	user, _ := middleware.UserFromContext(ctx)
	var actorID *int64
	if user != nil {
		actorID = &user.ID
	}
	ip := clientIP(r)
	if err := h.auditRec.Record(ctx, audit.Event{
		ActorUserID: actorID, ActorIP: ip,
		Action: action, TargetType: "team",
		TargetID: &id, Payload: payload,
	}); err != nil {
		h.logger.WarnContext(ctx, "audit record failed", "action", action, "err", err)
	}
}
