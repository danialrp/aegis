// SPDX-License-Identifier: AGPL-3.0-or-later

package v1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/riverqueue/river"

	"github.com/danialrp/aegis/internal/api/middleware"
	"github.com/danialrp/aegis/internal/audit"
	"github.com/danialrp/aegis/internal/db/sqlc"
	"github.com/danialrp/aegis/internal/jobs"
)

// SitesHandler serves /v1/sites/*.
type SitesHandler struct {
	queries     *sqlc.Queries
	auditRec    *audit.Recorder
	riverClient *river.Client[pgx.Tx]
	logger      *slog.Logger
}

// NewSitesHandler builds the handler. riverClient may be nil — Create
// then returns 503; useful for tests that only exercise the row CRUD.
func NewSitesHandler(q *sqlc.Queries, audit *audit.Recorder, rc *river.Client[pgx.Tx], logger *slog.Logger) *SitesHandler {
	return &SitesHandler{queries: q, auditRec: audit, riverClient: rc, logger: logger}
}

// --- shapes ---

type createSiteRequest struct {
	ServerID  int64  `json:"server_id"`
	Name      string `json:"name"`
	Domain    string `json:"domain"`
	SiteType  string `json:"site_type"`
	ProxyPort int    `json:"proxy_port,omitempty"` // required for docker
}

type siteResponse struct {
	ID             int64   `json:"id"`
	ServerID       int64   `json:"server_id"`
	Name           string  `json:"name"`
	Domain         string  `json:"domain"`
	SiteType       string  `json:"site_type"`
	Status         string  `json:"status"`
	WorkingDir     string  `json:"working_dir"`
	ProxyPort      *int32  `json:"proxy_port,omitempty"`
	ProvisionError *string `json:"provision_error,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

// Valid site types (matches the CHECK constraint on sites.site_type).
// Phase 1 covered static; Phase 3 covered docker; Phase 4 adds the
// PHP family (php/laravel/wordpress) and nextjs.
var validSiteTypes = map[string]struct{}{
	"static":    {},
	"php":       {},
	"laravel":   {},
	"wordpress": {},
	"nextjs":    {},
	"docker":    {},
}

// siteTypesNeedingProxyPort lists the types that point nginx at a
// localhost upstream rather than a filesystem root or PHP-FPM socket.
var siteTypesNeedingProxyPort = map[string]struct{}{
	"docker": {},
	"nextjs": {},
}

// --- handlers ---

// List handles GET /v1/sites.
func (h *SitesHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.queries.ListSites(r.Context())
	if err != nil {
		h.logger.ErrorContext(r.Context(), "list sites", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	out := make([]siteResponse, 0, len(rows))
	for _, s := range rows {
		out = append(out, toSiteResponse(s))
	}
	writeJSON(w, http.StatusOK, out)
}

// Get handles GET /v1/sites/{id}.
func (h *SitesHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}
	row, err := h.queries.GetSite(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		h.logger.ErrorContext(r.Context(), "get site", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, toSiteResponse(row))
}

// Create handles POST /v1/sites.
//
// Returns 201 with the row at status=pending. Host-side bootstrap
// (Linux user, nginx vhost) is wired in subsequent 1.x checkpoints.
func (h *SitesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createSiteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := validateCreateSite(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Verify the server exists. Without this the FK error surfaces as
	// a 500 with a Postgres-flavoured message — not what we want.
	if _, err := h.queries.GetServer(r.Context(), req.ServerID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "server_not_found")
			return
		}
		h.logger.ErrorContext(r.Context(), "lookup server", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// We don't know the id yet, so working_dir uses a sentinel that
	// gets fixed up immediately after insert. Two-step to keep the
	// API contract simple: every site row always has a non-empty
	// working_dir.
	row, err := h.queries.CreateSite(r.Context(), sqlc.CreateSiteParams{
		ServerID:   req.ServerID,
		Name:       req.Name,
		Domain:     req.Domain,
		SiteType:   req.SiteType,
		WorkingDir: "/tmp/aegis-pending",
	})
	if err != nil {
		// Unique-violation surfaces (server_id, domain) collisions
		// helpfully.
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "domain_already_used_on_server")
			return
		}
		h.logger.ErrorContext(r.Context(), "create site", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Now that we have the id, set the canonical working_dir.
	row.WorkingDir = fmt.Sprintf("/srv/sites/%d", row.ID)
	if err := h.queries.SetSiteWorkingDir(r.Context(), sqlc.SetSiteWorkingDirParams{
		ID: row.ID, WorkingDir: row.WorkingDir,
	}); err != nil {
		h.logger.WarnContext(r.Context(), "set working_dir", "err", err)
	}

	// docker + nextjs sites carry the upstream port the nginx vhost
	// forwards to.
	if _, needsPort := siteTypesNeedingProxyPort[req.SiteType]; needsPort && req.ProxyPort > 0 {
		port := pgtype.Int4{Int32: int32(req.ProxyPort), Valid: true} //nolint:gosec // validated 1..65535
		if err := h.queries.SetSiteProxyPort(r.Context(), sqlc.SetSiteProxyPortParams{
			ID: row.ID, ProxyPort: port,
		}); err != nil {
			h.logger.WarnContext(r.Context(), "set proxy_port", "err", err)
		} else {
			row.ProxyPort = port
		}
	}

	// Enqueue host-side provisioning. Without a river client (e.g. in
	// tests), we leave the row at status=pending so callers can still
	// observe creation; in prod main wires the client.
	if h.riverClient != nil {
		if _, err := h.riverClient.Insert(r.Context(), jobs.ProvisionSiteArgs{
			SiteID: row.ID,
		}, nil); err != nil {
			h.logger.ErrorContext(r.Context(), "enqueue site provision job", "err", err)
			// Don't fail the request — the row exists, the operator can
			// retry from the UI in a later checkpoint. Audit the gap.
			h.recordAudit(r.Context(), r, row.ID, "site.provision.enqueue_failed",
				map[string]string{"error": err.Error()})
		}
	}

	h.recordAudit(r.Context(), r, row.ID, "site.create",
		map[string]any{"server_id": req.ServerID, "domain": req.Domain})

	writeJSON(w, http.StatusCreated, toSiteResponse(row))
}

// Delete handles DELETE /v1/sites/{id}.
//
// 1.1 scope: removes the row only. Host-side teardown lands in 1.2+.
func (h *SitesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}
	if _, err := h.queries.GetSite(r.Context(), id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if err := h.queries.DeleteSite(r.Context(), id); err != nil {
		h.logger.ErrorContext(r.Context(), "delete site", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	h.recordAudit(r.Context(), r, id, "site.delete", nil)
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func validateCreateSite(req *createSiteRequest) error {
	if req.ServerID <= 0 {
		return errors.New("server_id_required")
	}
	if strings.TrimSpace(req.Name) == "" {
		return errors.New("name_required")
	}
	if strings.TrimSpace(req.Domain) == "" {
		return errors.New("domain_required")
	}
	if _, ok := validSiteTypes[req.SiteType]; !ok {
		return errors.New("invalid_site_type")
	}
	if _, needsPort := siteTypesNeedingProxyPort[req.SiteType]; needsPort {
		if req.ProxyPort < 1 || req.ProxyPort > 65535 {
			return errors.New("proxy_port_required")
		}
	}
	return nil
}

func toSiteResponse(s sqlc.Site) siteResponse {
	r := siteResponse{
		ID:         s.ID,
		ServerID:   s.ServerID,
		Name:       s.Name,
		Domain:     s.Domain,
		SiteType:   s.SiteType,
		Status:     s.ProvisionStatus,
		WorkingDir: s.WorkingDir,
		CreatedAt:  tsString(s.CreatedAt),
		UpdatedAt:  tsString(s.UpdatedAt),
	}
	if s.ProxyPort.Valid {
		v := s.ProxyPort.Int32
		r.ProxyPort = &v
	}
	if s.ProvisionError.Valid {
		v := s.ProvisionError.String
		r.ProvisionError = &v
	}
	return r
}

// isUniqueViolation reports whether err is a Postgres 23505
// unique-violation. We string-match because the typed check would
// pull in pgconn; the string is documented stable.
func isUniqueViolation(err error) bool {
	return strings.Contains(err.Error(), "SQLSTATE 23505")
}

func (h *SitesHandler) recordAudit(ctx context.Context, r *http.Request, siteID int64, action string, payload any) {
	id := siteID
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
		TargetType:  "site",
		TargetID:    &id,
		Payload:     payload,
	}); err != nil {
		h.logger.WarnContext(ctx, "audit record failed", "action", action, "err", err)
	}
}
