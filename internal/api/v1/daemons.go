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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/danialrp/aegis/internal/agentbus"
	"github.com/danialrp/aegis/internal/api/middleware"
	"github.com/danialrp/aegis/internal/audit"
	"github.com/danialrp/aegis/internal/db/sqlc"
	"github.com/danialrp/aegis/pkg/protocol"
)

// DaemonsHandler serves /v1/sites/{id}/daemons/*.
//
// Unlike cert issuance, daemon RPCs (write, action, logs) are fast
// enough to dispatch synchronously from the HTTP handler — they don't
// need river. We keep the call inside a generous timeout.
type DaemonsHandler struct {
	queries  *sqlc.Queries
	auditRec *audit.Recorder
	hub      *agentbus.Hub
	logger   *slog.Logger
}

// NewDaemonsHandler builds the handler.
func NewDaemonsHandler(q *sqlc.Queries, a *audit.Recorder, hub *agentbus.Hub, logger *slog.Logger) *DaemonsHandler {
	return &DaemonsHandler{queries: q, auditRec: a, hub: hub, logger: logger}
}

// --- shapes ---

type createDaemonRequest struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Command     string `json:"command"`
	AutoRestart bool   `json:"auto_restart"`
}

type daemonActionRequest struct {
	Action string `json:"action"`
}

type daemonResponse struct {
	ID           int64   `json:"id"`
	SiteID       int64   `json:"site_id"`
	Slug         string  `json:"slug"`
	Name         string  `json:"name"`
	Command      string  `json:"command"`
	AutoRestart  bool    `json:"auto_restart"`
	Status       string  `json:"status"`
	LastError    *string `json:"last_error,omitempty"`
	LastActionAt *string `json:"last_action_at,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

// --- handlers ---

// List handles GET /v1/sites/{id}/daemons.
func (h *DaemonsHandler) List(w http.ResponseWriter, r *http.Request) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	rows, err := h.queries.ListSiteDaemonsForSite(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	out := make([]daemonResponse, 0, len(rows))
	for _, d := range rows {
		out = append(out, daemonResponseFromRow(d))
	}
	writeJSON(w, http.StatusOK, out)
}

// Create handles POST /v1/sites/{id}/daemons.
func (h *DaemonsHandler) Create(w http.ResponseWriter, r *http.Request) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	var req createDaemonRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := validateDaemonRequest(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	site, err := h.queries.GetSite(r.Context(), siteID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "site_not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	row, err := h.queries.CreateSiteDaemon(r.Context(), sqlc.CreateSiteDaemonParams{
		SiteID:      siteID,
		Slug:        req.Slug,
		Name:        req.Name,
		Command:     req.Command,
		AutoRestart: req.AutoRestart,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "slug_already_used_on_site")
			return
		}
		h.logger.ErrorContext(r.Context(), "create site daemon", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	if err := h.callDaemonWrite(r.Context(), site.ServerID, row); err != nil {
		h.markFailure(r.Context(), row.ID, err)
		writeJSON(w, http.StatusAccepted, daemonResponseFromRow(row))
		return
	}
	_ = h.queries.UpdateSiteDaemonStatus(r.Context(), sqlc.UpdateSiteDaemonStatusParams{
		ID: row.ID, Status: "running",
	})

	h.recordAudit(r.Context(), r, row.ID, "site.daemon.created",
		map[string]any{"site_id": siteID, "slug": req.Slug})

	updated, _ := h.queries.GetSiteDaemon(r.Context(), row.ID)
	writeJSON(w, http.StatusCreated, daemonResponseFromRow(updated))
}

// Action handles POST /v1/sites/{id}/daemons/{daemon_id}/action.
func (h *DaemonsHandler) Action(w http.ResponseWriter, r *http.Request) {
	daemonID, err := strconv.ParseInt(chi.URLParam(r, "daemon_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}
	var req daemonActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	switch req.Action {
	case "start", "stop", "restart":
	default:
		writeError(w, http.StatusBadRequest, "invalid_action")
		return
	}

	d, site, err := h.fetchDaemonAndSite(r.Context(), daemonID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}

	conn, ok := h.hub.Get(site.ServerID)
	if !ok {
		h.markFailure(r.Context(), daemonID, errors.New("no live agent"))
		writeError(w, http.StatusServiceUnavailable, "agent_offline")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if _, err := conn.Request(ctx, protocol.MethodHostDaemonAction, protocol.DaemonActionParams{
		SiteID: site.ID, Slug: d.Slug, Action: req.Action,
	}); err != nil {
		h.markFailure(r.Context(), daemonID, err)
		writeError(w, http.StatusBadGateway, "rpc_failed")
		return
	}

	status := map[string]string{"start": "running", "stop": "stopped", "restart": "running"}[req.Action]
	_ = h.queries.UpdateSiteDaemonStatus(r.Context(), sqlc.UpdateSiteDaemonStatusParams{
		ID: daemonID, Status: status,
	})
	h.recordAudit(r.Context(), r, daemonID, "site.daemon."+req.Action,
		map[string]any{"slug": d.Slug})

	updated, _ := h.queries.GetSiteDaemon(r.Context(), daemonID)
	writeJSON(w, http.StatusOK, daemonResponseFromRow(updated))
}

// Logs handles GET /v1/sites/{id}/daemons/{daemon_id}/logs.
func (h *DaemonsHandler) Logs(w http.ResponseWriter, r *http.Request) {
	daemonID, err := strconv.ParseInt(chi.URLParam(r, "daemon_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}
	d, site, err := h.fetchDaemonAndSite(r.Context(), daemonID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	conn, ok := h.hub.Get(site.ServerID)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "agent_offline")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	resp, err := conn.Request(ctx, protocol.MethodHostDaemonLogs, protocol.DaemonSlugParams{
		SiteID: site.ID, Slug: d.Slug, Lines: 500,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "rpc_failed")
		return
	}
	var result protocol.DaemonLogsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		writeError(w, http.StatusInternalServerError, "decode_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"output": result.Output})
}

// Delete handles DELETE /v1/sites/{id}/daemons/{daemon_id}.
func (h *DaemonsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	daemonID, err := strconv.ParseInt(chi.URLParam(r, "daemon_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}
	d, site, err := h.fetchDaemonAndSite(r.Context(), daemonID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	// Best effort: ask the agent to remove the supervisor config.
	// We tolerate failure (agent offline, supervisor never wrote it).
	if conn, ok := h.hub.Get(site.ServerID); ok {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		_, _ = conn.Request(ctx, protocol.MethodHostDaemonRemove, protocol.DaemonSlugParams{
			SiteID: site.ID, Slug: d.Slug,
		})
		cancel()
	}
	if err := h.queries.DeleteSiteDaemon(r.Context(), daemonID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	h.recordAudit(r.Context(), r, daemonID, "site.daemon.deleted", nil)
	w.WriteHeader(http.StatusNoContent)
}

// --- internals ---

func (h *DaemonsHandler) callDaemonWrite(ctx context.Context, serverID int64, d sqlc.SiteDaemon) error {
	conn, ok := h.hub.Get(serverID)
	if !ok {
		return errors.New("no live agent")
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err := conn.Request(callCtx, protocol.MethodHostDaemonWrite, protocol.DaemonWriteParams{
		SiteID:      d.SiteID,
		Slug:        d.Slug,
		Command:     d.Command,
		AutoRestart: d.AutoRestart,
	})
	return err
}

func (h *DaemonsHandler) fetchDaemonAndSite(ctx context.Context, daemonID int64) (sqlc.SiteDaemon, sqlc.Site, error) {
	d, err := h.queries.GetSiteDaemon(ctx, daemonID)
	if err != nil {
		return sqlc.SiteDaemon{}, sqlc.Site{}, err
	}
	site, err := h.queries.GetSite(ctx, d.SiteID)
	return d, site, err
}

func (h *DaemonsHandler) markFailure(ctx context.Context, daemonID int64, cause error) {
	_ = h.queries.UpdateSiteDaemonStatus(ctx, sqlc.UpdateSiteDaemonStatusParams{
		ID:        daemonID,
		Status:    "error",
		LastError: pgtype.Text{String: cause.Error(), Valid: true},
	})
}

func validateDaemonRequest(req *createDaemonRequest) error {
	req.Slug = strings.TrimSpace(req.Slug)
	if req.Slug == "" {
		return errors.New("slug_required")
	}
	for _, r := range req.Slug {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return errors.New("slug_invalid_chars")
		}
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = req.Slug
	}
	if strings.TrimSpace(req.Command) == "" {
		return errors.New("command_required")
	}
	return nil
}

func daemonResponseFromRow(d sqlc.SiteDaemon) daemonResponse {
	r := daemonResponse{
		ID:          d.ID,
		SiteID:      d.SiteID,
		Slug:        d.Slug,
		Name:        d.Name,
		Command:     d.Command,
		AutoRestart: d.AutoRestart,
		Status:      d.Status,
		CreatedAt:   tsString(d.CreatedAt),
	}
	if d.LastError.Valid {
		v := d.LastError.String
		r.LastError = &v
	}
	if d.LastActionAt.Valid {
		v := tsStringFromPg(d.LastActionAt)
		r.LastActionAt = &v
	}
	return r
}

func (h *DaemonsHandler) recordAudit(ctx context.Context, r *http.Request, targetID int64, action string, payload any) {
	id := targetID
	user, _ := middleware.UserFromContext(ctx)
	var actorID *int64
	if user != nil {
		actorID = &user.ID
	}
	ip := clientIP(r)
	if err := h.auditRec.Record(ctx, audit.Event{
		ActorUserID: actorID, ActorIP: ip,
		Action: action, TargetType: "site_daemon",
		TargetID: &id, Payload: payload,
	}); err != nil {
		h.logger.WarnContext(ctx, "audit record failed", "action", action, "err", err)
	}
}
