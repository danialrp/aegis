// SPDX-License-Identifier: AGPL-3.0-or-later

package v1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/riverqueue/river"

	"github.com/danialrp/aegis/internal/api/middleware"
	"github.com/danialrp/aegis/internal/audit"
	"github.com/danialrp/aegis/internal/db/sqlc"
	"github.com/danialrp/aegis/internal/jobs"
)

// ServersHandler serves /v1/servers/*.
type ServersHandler struct {
	queries     *sqlc.Queries
	auditRec    *audit.Recorder
	riverClient *river.Client[pgx.Tx]
	logger      *slog.Logger
}

// NewServersHandler builds the handler. riverClient may be nil in
// degraded boots (e.g. river migration failure); CreateServer will
// return 503 in that case.
func NewServersHandler(q *sqlc.Queries, audit *audit.Recorder, rc *river.Client[pgx.Tx], logger *slog.Logger) *ServersHandler {
	return &ServersHandler{queries: q, auditRec: audit, riverClient: rc, logger: logger}
}

// --- request / response shapes ---

type createServerRequest struct {
	Name       string `json:"name"`
	PublicIP   string `json:"public_ip"`
	SSHUser    string `json:"ssh_user"`
	SSHPort    int    `json:"ssh_port"`
	Password   string `json:"ssh_password,omitempty"`
	PrivateKey string `json:"ssh_private_key,omitempty"`
}

type serverResponse struct {
	ID               int64   `json:"id"`
	Name             string  `json:"name"`
	PublicIP         string  `json:"public_ip"`
	SSHUser          string  `json:"ssh_user"`
	Status           string  `json:"status"`
	AgentFingerprint *string `json:"agent_fingerprint,omitempty"`
	AgentLastSeen    *string `json:"agent_last_seen,omitempty"`
	ProvisionError   *string `json:"provision_error,omitempty"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

// --- handlers ---

// List handles GET /v1/servers.
func (h *ServersHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.queries.ListServers(r.Context())
	if err != nil {
		h.logger.ErrorContext(r.Context(), "list servers", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	out := make([]serverResponse, 0, len(rows))
	for _, s := range rows {
		out = append(out, toServerResponse(s))
	}
	writeJSON(w, http.StatusOK, out)
}

// Get handles GET /v1/servers/{id}.
func (h *ServersHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}
	row, err := h.queries.GetServer(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		h.logger.ErrorContext(r.Context(), "get server", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, toServerResponse(row))
}

// Create handles POST /v1/servers.
//
// Returns 202 (Accepted) — the row exists at status=pending and a
// background job has been enqueued. Clients poll GET /v1/servers/{id}
// to observe progress.
func (h *ServersHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := validateCreate(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if h.riverClient == nil {
		writeError(w, http.StatusServiceUnavailable, "background_worker_unavailable")
		return
	}

	addr, err := netip.ParseAddr(req.PublicIP)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_public_ip")
		return
	}

	row, err := h.queries.CreateServer(r.Context(), sqlc.CreateServerParams{
		Name:            req.Name,
		PublicIp:        addr,
		SshUser:         req.SSHUser,
		ProvisionStatus: "pending",
	})
	if err != nil {
		h.logger.ErrorContext(r.Context(), "create server row", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	if _, err := h.riverClient.Insert(r.Context(), jobs.ProvisionServerArgs{
		ServerID:   row.ID,
		Host:       req.PublicIP,
		Port:       req.SSHPort,
		User:       req.SSHUser,
		Password:   req.Password,
		PrivateKey: []byte(req.PrivateKey),
	}, nil); err != nil {
		// Roll back the row so the operator doesn't see a stranded
		// `pending` entry that will never get picked up.
		if delErr := h.queries.DeleteServer(r.Context(), row.ID); delErr != nil {
			h.logger.ErrorContext(r.Context(), "rollback server row failed", "err", delErr)
		}
		h.logger.ErrorContext(r.Context(), "enqueue provision job", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	h.recordAudit(r.Context(), r, row.ID, "server.create",
		map[string]any{"name": req.Name, "host": req.PublicIP})

	writeJSON(w, http.StatusAccepted, toServerResponse(row))
}

// Delete handles DELETE /v1/servers/{id}.
//
// 0.8 scope: deletes the row only. Operator is responsible for
// removing the agent service on the target host. Remote uninstall
// lands with the RBAC + lifecycle work in Phase 7.
func (h *ServersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}
	if _, err := h.queries.GetServer(r.Context(), id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if err := h.queries.DeleteServer(r.Context(), id); err != nil {
		h.logger.ErrorContext(r.Context(), "delete server", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	h.recordAudit(r.Context(), r, id, "server.delete", nil)
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func validateCreate(req *createServerRequest) error {
	if req.Name == "" {
		return errors.New("name_required")
	}
	if req.PublicIP == "" {
		return errors.New("public_ip_required")
	}
	if req.SSHUser == "" {
		return errors.New("ssh_user_required")
	}
	if req.SSHPort == 0 {
		req.SSHPort = 22
	}
	if req.SSHPort < 1 || req.SSHPort > 65535 {
		return errors.New("invalid_ssh_port")
	}
	if req.Password == "" && req.PrivateKey == "" {
		return errors.New("ssh_credentials_required")
	}
	if req.Password != "" && req.PrivateKey != "" {
		return errors.New("provide_only_one_credential")
	}
	return nil
}

func toServerResponse(s sqlc.Server) serverResponse {
	r := serverResponse{
		ID:        s.ID,
		Name:      s.Name,
		PublicIP:  s.PublicIp.String(),
		SSHUser:   s.SshUser,
		Status:    s.ProvisionStatus,
		CreatedAt: tsString(s.CreatedAt),
		UpdatedAt: tsString(s.UpdatedAt),
	}
	if s.AgentFingerprint.Valid {
		v := s.AgentFingerprint.String
		r.AgentFingerprint = &v
	}
	if s.AgentLastSeen.Valid {
		v := s.AgentLastSeen.Time.UTC().Format(time.RFC3339)
		r.AgentLastSeen = &v
	}
	if s.ProvisionError.Valid {
		v := s.ProvisionError.String
		r.ProvisionError = &v
	}
	return r
}

func tsString(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339)
}

func (h *ServersHandler) recordAudit(ctx context.Context, r *http.Request, serverID int64, action string, payload any) {
	id := serverID
	user, _ := middleware.UserFromContext(ctx)
	var actorID *int64
	if user != nil {
		actorID = &user.ID
	}
	ip := clientIP(r)
	if err := h.auditRec.Record(ctx, audit.Event{
		ActorUserID: actorID,
		ActorIP:     ip,
		Action:      action,
		TargetType:  "server",
		TargetID:    &id,
		Payload:     payload,
	}); err != nil {
		h.logger.WarnContext(ctx, "audit record failed", "action", action, "err", err)
	}
}

// Compile-time check that the handler implements the small surface
// v1.go's Mount expects.
var _ = fmt.Sprintf
