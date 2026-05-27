// SPDX-License-Identifier: AGPL-3.0-or-later

package v1

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/danialrp/aegis/internal/api/middleware"
	"github.com/danialrp/aegis/internal/audit"
	"github.com/danialrp/aegis/internal/db/sqlc"
)

// SitePermissionsHandler serves /v1/sites/{id}/permissions/*.
type SitePermissionsHandler struct {
	queries  *sqlc.Queries
	auditRec *audit.Recorder
	logger   *slog.Logger
}

// NewSitePermissionsHandler builds the handler.
func NewSitePermissionsHandler(q *sqlc.Queries, a *audit.Recorder, logger *slog.Logger) *SitePermissionsHandler {
	return &SitePermissionsHandler{queries: q, auditRec: a, logger: logger}
}

// --- shapes ---

type permFlags struct {
	Read     bool `json:"read"`
	Execute  bool `json:"execute"`
	Write    bool `json:"write"`
	Logs     bool `json:"logs"`
	Terminal bool `json:"terminal"`
	Inspect  bool `json:"inspect"`
}

type upsertPermissionRequest struct {
	UserID *int64 `json:"user_id,omitempty"`
	TeamID *int64 `json:"team_id,omitempty"`
	permFlags
}

type permissionResponse struct {
	ID     int64  `json:"id"`
	SiteID int64  `json:"site_id"`
	UserID *int64 `json:"user_id,omitempty"`
	TeamID *int64 `json:"team_id,omitempty"`
	permFlags
}

// --- handlers ---

// List handles GET /v1/sites/{id}/permissions.
func (h *SitePermissionsHandler) List(w http.ResponseWriter, r *http.Request) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	rows, err := h.queries.ListSitePermissions(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	out := make([]permissionResponse, 0, len(rows))
	for _, p := range rows {
		out = append(out, sitePermToResponse(p))
	}
	writeJSON(w, http.StatusOK, out)
}

// Upsert handles POST /v1/sites/{id}/permissions. Exactly one of
// user_id / team_id must be set.
func (h *SitePermissionsHandler) Upsert(w http.ResponseWriter, r *http.Request) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	var req upsertPermissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if (req.UserID == nil) == (req.TeamID == nil) {
		writeError(w, http.StatusBadRequest, "exactly_one_of_user_or_team")
		return
	}

	var row sqlc.SitePermission
	var err error
	switch {
	case req.UserID != nil:
		row, err = h.queries.UpsertSiteUserPermission(r.Context(), sqlc.UpsertSiteUserPermissionParams{
			SiteID:       siteID,
			UserID:       pgInt8(*req.UserID),
			PermRead:     req.Read,
			PermExecute:  req.Execute,
			PermWrite:    req.Write,
			PermLogs:     req.Logs,
			PermTerminal: req.Terminal,
			PermInspect:  req.Inspect,
		})
	case req.TeamID != nil:
		row, err = h.queries.UpsertSiteTeamPermission(r.Context(), sqlc.UpsertSiteTeamPermissionParams{
			SiteID:       siteID,
			TeamID:       pgInt8(*req.TeamID),
			PermRead:     req.Read,
			PermExecute:  req.Execute,
			PermWrite:    req.Write,
			PermLogs:     req.Logs,
			PermTerminal: req.Terminal,
			PermInspect:  req.Inspect,
		})
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	h.recordAudit(r.Context(), r, siteID, "site.permission.upserted",
		map[string]any{"user_id": req.UserID, "team_id": req.TeamID})
	writeJSON(w, http.StatusOK, sitePermToResponse(row))
}

// Delete handles DELETE /v1/sites/{id}/permissions/{perm_id}.
func (h *SitePermissionsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	permID, err := strconv.ParseInt(chi.URLParam(r, "perm_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}
	if err := h.queries.DeleteSitePermission(r.Context(), sqlc.DeleteSitePermissionParams{
		ID: permID, SiteID: siteID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	h.recordAudit(r.Context(), r, siteID, "site.permission.deleted",
		map[string]any{"perm_id": permID})
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func sitePermToResponse(p sqlc.SitePermission) permissionResponse {
	r := permissionResponse{
		ID:     p.ID,
		SiteID: p.SiteID,
		permFlags: permFlags{
			Read:     p.PermRead,
			Execute:  p.PermExecute,
			Write:    p.PermWrite,
			Logs:     p.PermLogs,
			Terminal: p.PermTerminal,
			Inspect:  p.PermInspect,
		},
	}
	if p.UserID.Valid {
		v := p.UserID.Int64
		r.UserID = &v
	}
	if p.TeamID.Valid {
		v := p.TeamID.Int64
		r.TeamID = &v
	}
	return r
}

func (h *SitePermissionsHandler) recordAudit(ctx context.Context, r *http.Request, siteID int64, action string, payload any) {
	id := siteID
	user, _ := middleware.UserFromContext(ctx)
	var actorID *int64
	if user != nil {
		actorID = &user.ID
	}
	ip := clientIP(r)
	if err := h.auditRec.Record(ctx, audit.Event{
		ActorUserID: actorID, ActorIP: ip,
		Action: action, TargetType: "site_permission",
		TargetID: &id, Payload: payload,
	}); err != nil {
		h.logger.WarnContext(ctx, "audit record failed", "action", action, "err", err)
	}
}
