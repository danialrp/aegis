// SPDX-License-Identifier: AGPL-3.0-or-later

package v1

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/riverqueue/river"

	"github.com/danialrp/aegis/internal/api/middleware"
	"github.com/danialrp/aegis/internal/audit"
	"github.com/danialrp/aegis/internal/db/sqlc"
	"github.com/danialrp/aegis/internal/jobs"
)

// SSLHandler serves /v1/sites/{id}/certs/*.
type SSLHandler struct {
	queries          *sqlc.Queries
	auditRec         *audit.Recorder
	riverClient      *river.Client[pgx.Tx]
	logger           *slog.Logger
	letsEncryptEmail string
}

// NewSSLHandler builds the handler. letsEncryptEmail comes from
// AEGIS_LETSENCRYPT_EMAIL config; empty means SSL issuance returns 503
// with a clear error.
func NewSSLHandler(q *sqlc.Queries, a *audit.Recorder, rc *river.Client[pgx.Tx], email string, logger *slog.Logger) *SSLHandler {
	return &SSLHandler{
		queries: q, auditRec: a, riverClient: rc,
		letsEncryptEmail: email, logger: logger,
	}
}

type certResponse struct {
	ID        int64   `json:"id"`
	SiteID    int64   `json:"site_id"`
	Domain    string  `json:"domain"`
	Status    string  `json:"status"`
	IssuedAt  *string `json:"issued_at,omitempty"`
	ExpiresAt *string `json:"expires_at,omitempty"`
	LastError *string `json:"last_error,omitempty"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

// ListCerts handles GET /v1/sites/{id}/certs.
func (h *SSLHandler) ListCerts(w http.ResponseWriter, r *http.Request) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	rows, err := h.queries.ListSiteCertsForSite(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	out := make([]certResponse, 0, len(rows))
	for _, c := range rows {
		out = append(out, certResponseFromRow(c))
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateCert handles POST /v1/sites/{id}/certs.
//
// Enqueues an IssueCert job. Domain comes from the site's own domain
// (1.x ships single-domain sites). Returns 202 with the cert row at
// status=pending; UI polls.
func (h *SSLHandler) CreateCert(w http.ResponseWriter, r *http.Request) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	if h.riverClient == nil {
		writeError(w, http.StatusServiceUnavailable, "background_worker_unavailable")
		return
	}
	if h.letsEncryptEmail == "" {
		writeError(w, http.StatusServiceUnavailable, "letsencrypt_email_not_configured")
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

	row, err := h.queries.CreateSiteCert(r.Context(), sqlc.CreateSiteCertParams{
		SiteID: siteID, Domain: site.Domain,
	})
	if err != nil {
		h.logger.ErrorContext(r.Context(), "create site cert", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	if _, err := h.riverClient.Insert(r.Context(), jobs.IssueCertArgs{
		CertID: row.ID, Email: h.letsEncryptEmail,
	}, nil); err != nil {
		h.logger.ErrorContext(r.Context(), "enqueue issue_cert", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	h.recordAudit(r.Context(), r, row.ID, "site.cert.requested",
		map[string]any{"site_id": siteID, "domain": site.Domain})
	writeJSON(w, http.StatusAccepted, certResponseFromRow(row))
}

// DeleteCert handles DELETE /v1/sites/{site_id}/certs/{cert_id}.
//
// 2.x scope: marks the cert row as removed in the DB. Remote revocation
// is a stretch goal (cert_remove helper exists, but operator may want
// the cert active during a UI cleanup window).
func (h *SSLHandler) DeleteCert(w http.ResponseWriter, r *http.Request) {
	certID, err := strconv.ParseInt(chi.URLParam(r, "cert_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}
	if err := h.queries.DeleteSiteCert(r.Context(), certID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	h.recordAudit(r.Context(), r, certID, "site.cert.deleted", nil)
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func certResponseFromRow(c sqlc.SiteCert) certResponse {
	r := certResponse{
		ID:        c.ID,
		SiteID:    c.SiteID,
		Domain:    c.Domain,
		Status:    c.Status,
		CreatedAt: tsString(c.CreatedAt),
		UpdatedAt: tsString(c.UpdatedAt),
	}
	if c.IssuedAt.Valid {
		v := tsStringFromPg(c.IssuedAt)
		r.IssuedAt = &v
	}
	if c.ExpiresAt.Valid {
		v := tsStringFromPg(c.ExpiresAt)
		r.ExpiresAt = &v
	}
	if c.LastError.Valid {
		v := c.LastError.String
		r.LastError = &v
	}
	return r
}

func tsStringFromPg(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format("2006-01-02T15:04:05Z")
}

func (h *SSLHandler) recordAudit(ctx context.Context, r *http.Request, targetID int64, action string, payload any) {
	id := targetID
	user, _ := middleware.UserFromContext(ctx)
	var actorID *int64
	if user != nil {
		actorID = &user.ID
	}
	ip := clientIP(r)
	if err := h.auditRec.Record(ctx, audit.Event{
		ActorUserID: actorID, ActorIP: ip,
		Action: action, TargetType: "site_cert",
		TargetID: &id, Payload: payload,
	}); err != nil {
		h.logger.WarnContext(ctx, "audit record failed", "action", action, "err", err)
	}
	_ = json.Marshal // placate goimports if other usage shifts
}
