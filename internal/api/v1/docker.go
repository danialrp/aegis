// SPDX-License-Identifier: AGPL-3.0-or-later

package v1

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
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

// DockerHandler serves /v1/sites/{id}/compose, /containers, /actions
// for docker-type sites.
type DockerHandler struct {
	queries  *sqlc.Queries
	auditRec *audit.Recorder
	hub      *agentbus.Hub
	logger   *slog.Logger
}

// NewDockerHandler builds the handler.
func NewDockerHandler(q *sqlc.Queries, a *audit.Recorder, hub *agentbus.Hub, logger *slog.Logger) *DockerHandler {
	return &DockerHandler{queries: q, auditRec: a, hub: hub, logger: logger}
}

// --- shapes ---

type composeResponse struct {
	SiteID    int64  `json:"site_id"`
	Body      string `json:"body"`
	UpdatedAt string `json:"updated_at"`
}

type putComposeRequest struct {
	Body string `json:"body"`
}

type composeActionRequest struct {
	Action string `json:"action"`
}

type containerRow struct {
	Name    string `json:"name"`
	Service string `json:"service"`
	Image   string `json:"image"`
	State   string `json:"state"`
	Status  string `json:"status"`
}

// --- compose CRUD ---

// GetCompose handles GET /v1/sites/{id}/compose.
func (h *DockerHandler) GetCompose(w http.ResponseWriter, r *http.Request) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	row, err := h.queries.GetSiteCompose(r.Context(), siteID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, composeResponse{
				SiteID: siteID,
				Body:   defaultComposeBody,
			})
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, composeResponse{
		SiteID:    row.SiteID,
		Body:      row.Body,
		UpdatedAt: tsString(row.UpdatedAt),
	})
}

// PutCompose handles PUT /v1/sites/{id}/compose. Stores in DB and
// asks the agent to mirror to /srv/sites/<id>/compose.yml.
func (h *DockerHandler) PutCompose(w http.ResponseWriter, r *http.Request) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	var req putComposeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if len(req.Body) > 256*1024 {
		writeError(w, http.StatusBadRequest, "compose_too_large")
		return
	}
	site, err := h.queries.GetSite(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusNotFound, "site_not_found")
		return
	}
	if site.SiteType != "docker" {
		writeError(w, http.StatusBadRequest, "site_not_docker")
		return
	}

	row, err := h.queries.UpsertSiteCompose(r.Context(), sqlc.UpsertSiteComposeParams{
		SiteID: siteID, Body: req.Body,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Best-effort: mirror to disk on the host. UI displays a warning
	// if this fails (e.g. agent offline); operator can re-save.
	if err := h.mirrorCompose(r.Context(), site, req.Body); err != nil {
		h.logger.WarnContext(r.Context(), "mirror compose failed", "err", err)
	}

	h.recordAudit(r.Context(), r, siteID, "site.compose.updated", nil)
	writeJSON(w, http.StatusOK, composeResponse{
		SiteID:    row.SiteID,
		Body:      row.Body,
		UpdatedAt: tsString(row.UpdatedAt),
	})
}

func (h *DockerHandler) mirrorCompose(ctx context.Context, site sqlc.Site, body string) error {
	conn, ok := h.hub.Get(site.ServerID)
	if !ok {
		return errors.New("agent offline")
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := conn.Request(callCtx, protocol.MethodHostComposeWrite, protocol.ComposeWriteParams{
		SiteID: site.ID, Body: body,
	})
	return err
}

// --- compose actions ---

// Action handles POST /v1/sites/{id}/compose/action.
func (h *DockerHandler) Action(w http.ResponseWriter, r *http.Request) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	var req composeActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	switch req.Action {
	case "up", "down", "restart", "pull", "build":
	default:
		writeError(w, http.StatusBadRequest, "invalid_action")
		return
	}
	site, err := h.queries.GetSite(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusNotFound, "site_not_found")
		return
	}
	if site.SiteType != "docker" {
		writeError(w, http.StatusBadRequest, "site_not_docker")
		return
	}
	conn, ok := h.hub.Get(site.ServerID)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "agent_offline")
		return
	}
	// up/pull/build can be slow — generous deadline.
	timeout := 5 * time.Minute
	if req.Action == "down" || req.Action == "restart" {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	if _, err := conn.Request(ctx, protocol.MethodHostComposeAction, protocol.ComposeActionParams{
		SiteID: siteID, Action: req.Action,
	}); err != nil {
		h.logger.ErrorContext(r.Context(), "compose action failed", "err", err)
		writeError(w, http.StatusBadGateway, "rpc_failed")
		return
	}
	h.recordAudit(r.Context(), r, siteID, "site.compose."+req.Action, nil)
	w.WriteHeader(http.StatusNoContent)
}

// ListContainers handles GET /v1/sites/{id}/containers.
func (h *DockerHandler) ListContainers(w http.ResponseWriter, r *http.Request) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	site, err := h.queries.GetSite(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusNotFound, "site_not_found")
		return
	}
	conn, ok := h.hub.Get(site.ServerID)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "agent_offline")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	resp, err := conn.Request(ctx, protocol.MethodHostComposePs, protocol.SiteIDParams{SiteID: siteID})
	if err != nil {
		writeError(w, http.StatusBadGateway, "rpc_failed")
		return
	}
	var result protocol.ComposePsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		writeError(w, http.StatusInternalServerError, "decode_failed")
		return
	}
	writeJSON(w, http.StatusOK, parseComposePs(result.Raw))
}

// ContainerLogs handles GET /v1/sites/{id}/containers/{service}/logs.
func (h *DockerHandler) ContainerLogs(w http.ResponseWriter, r *http.Request) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	service := chi.URLParam(r, "service")
	site, err := h.queries.GetSite(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusNotFound, "site_not_found")
		return
	}
	conn, ok := h.hub.Get(site.ServerID)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "agent_offline")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	resp, err := conn.Request(ctx, protocol.MethodHostComposeLogs, protocol.ComposeLogsParams{
		SiteID: siteID, Service: service, Lines: 500,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "rpc_failed")
		return
	}
	var result protocol.ComposeLogsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		writeError(w, http.StatusInternalServerError, "decode_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"output": result.Output})
}

// --- helpers ---

// parseComposePs parses the raw `docker compose ps --format json`
// output. compose v2 emits one JSON object per line (NDJSON).
func parseComposePs(raw string) []containerRow {
	out := make([]containerRow, 0)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// compose v2 fields we care about — leave the rest unparsed.
		var entry struct {
			Name    string `json:"Name"`
			Service string `json:"Service"`
			Image   string `json:"Image"`
			State   string `json:"State"`
			Status  string `json:"Status"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			// Skip malformed line; don't abort the whole list.
			continue
		}
		out = append(out, containerRow(entry))
	}
	return out
}

const defaultComposeBody = `# Aegis docker site — define one or more services here. Aegis runs
# them with project name aegis-site-<this-site-id> and proxies the
# domain to whichever service binds to the configured proxy_port.
services:
  app:
    image: nginx:alpine
    restart: unless-stopped
    ports:
      - "127.0.0.1:8081:80"   # match the site's proxy_port
`

func (h *DockerHandler) recordAudit(ctx context.Context, r *http.Request, targetID int64, action string, payload any) {
	id := targetID
	user, _ := middleware.UserFromContext(ctx)
	var actorID *int64
	if user != nil {
		actorID = &user.ID
	}
	ip := clientIP(r)
	if err := h.auditRec.Record(ctx, audit.Event{
		ActorUserID: actorID, ActorIP: ip,
		Action: action, TargetType: "site_compose",
		TargetID: &id, Payload: payload,
	}); err != nil {
		h.logger.WarnContext(ctx, "audit record failed", "action", action, "err", err)
	}
}

// (silence linter if pgtype is unused after refactors)
var _ = pgtype.Text{}
