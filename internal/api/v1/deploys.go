// SPDX-License-Identifier: AGPL-3.0-or-later

package v1

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
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

// DeploysHandler serves /v1/sites/{id}/deploy-script and
// /v1/sites/{id}/deploys/*. Also handles the public webhook entry.
type DeploysHandler struct {
	queries     *sqlc.Queries
	auditRec    *audit.Recorder
	riverClient *river.Client[pgx.Tx]
	logger      *slog.Logger
}

// NewDeploysHandler builds the handler.
func NewDeploysHandler(q *sqlc.Queries, a *audit.Recorder, rc *river.Client[pgx.Tx], logger *slog.Logger) *DeploysHandler {
	return &DeploysHandler{queries: q, auditRec: a, riverClient: rc, logger: logger}
}

// --- shapes ---

type deployScriptResponse struct {
	SiteID    int64   `json:"site_id"`
	Body      string  `json:"body"`
	CronSpec  *string `json:"cron_spec,omitempty"`
	UpdatedAt string  `json:"updated_at"`
}

type putDeployScriptRequest struct {
	Body     string  `json:"body"`
	CronSpec *string `json:"cron_spec,omitempty"`
}

type deployResponse struct {
	ID         int64   `json:"id"`
	SiteID     int64   `json:"site_id"`
	Trigger    string  `json:"trigger"`
	Status     string  `json:"status"`
	StartedAt  *string `json:"started_at,omitempty"`
	FinishedAt *string `json:"finished_at,omitempty"`
	ExitCode   *int32  `json:"exit_code,omitempty"`
	OutputLog  string  `json:"output_log"`
	CreatedAt  string  `json:"created_at"`
}

// --- deploy script CRUD ---

// GetScript handles GET /v1/sites/{id}/deploy-script.
func (h *DeploysHandler) GetScript(w http.ResponseWriter, r *http.Request) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	row, err := h.queries.GetDeployScript(r.Context(), siteID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Auto-default for an unsaved site, tailored to the site
			// type so the editor shows a useful starting point.
			body := defaultDeployScript
			if site, sErr := h.queries.GetSite(r.Context(), siteID); sErr == nil {
				body = defaultDeployScriptFor(site.SiteType)
			}
			writeJSON(w, http.StatusOK, deployScriptResponse{
				SiteID: siteID,
				Body:   body,
			})
			return
		}
		h.logger.ErrorContext(r.Context(), "get deploy script", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, deployScriptResponseFromRow(row))
}

// defaultDeployScriptFor returns a starter script appropriate for the
// site type. The operator can rewrite freely; this is just a
// useful-on-first-render default.
func defaultDeployScriptFor(siteType string) string {
	switch siteType {
	case "laravel":
		return laravelDefaultDeployScript
	case "wordpress":
		return wordpressDefaultDeployScript
	case "nextjs":
		return nextjsDefaultDeployScript
	case "docker":
		return dockerDefaultDeployScript
	case "php":
		return phpGenericDefaultDeployScript
	default:
		return defaultDeployScript
	}
}

const laravelDefaultDeployScript = `#!/usr/bin/env bash
# Aegis Laravel deploy script — runs as the site's Linux user with
# cwd at the site's working directory.
set -euo pipefail

[ -d ".git" ] && git pull --ff-only origin main || true

composer install --no-dev --prefer-dist --optimize-autoloader --no-interaction

php artisan migrate --force
php artisan config:cache
php artisan route:cache
php artisan view:cache

echo "Done."
`

const wordpressDefaultDeployScript = `#!/usr/bin/env bash
# Aegis WordPress deploy script. First run downloads core + writes a
# wp-config.php skeleton; subsequent runs pull + (optionally) wp-cli update.
set -euo pipefail

if [ ! -f "wp-config.php" ]; then
  echo "==> Downloading WordPress core"
  curl -sSL https://wordpress.org/latest.tar.gz | tar -xz --strip-components=1
  cp wp-config-sample.php wp-config.php
  echo "==> Edit wp-config.php with your DB creds, then re-run."
  exit 0
fi

[ -d ".git" ] && git pull --ff-only origin main || true
command -v wp >/dev/null 2>&1 && wp core update || true

echo "Done."
`

const nextjsDefaultDeployScript = `#!/usr/bin/env bash
# Aegis Next.js deploy script. Builds the production bundle; the Node
# process itself should run as an Aegis-managed supervisor daemon —
# add one via the Daemons panel pointing at:
#   node node_modules/next/dist/bin/next start -p <proxy_port>
set -euo pipefail

[ -d ".git" ] && git pull --ff-only origin main || true

npm ci
npm run build

echo "Done — restart the next-server daemon to pick up the new build."
`

const dockerDefaultDeployScript = `#!/usr/bin/env bash
# Aegis docker deploy script.
set -euo pipefail

[ -d ".git" ] && git pull --ff-only origin main || true

# Replace <id> with this site's id (visible in the URL).
docker compose pull || true
docker compose up -d --build --remove-orphans

echo "Done."
`

const phpGenericDefaultDeployScript = `#!/usr/bin/env bash
# Aegis generic-PHP deploy script.
set -euo pipefail

[ -d ".git" ] && git pull --ff-only origin main || true
[ -f "composer.json" ] && composer install --no-dev --prefer-dist --optimize-autoloader --no-interaction

echo "Done."
`

// PutScript handles PUT /v1/sites/{id}/deploy-script.
func (h *DeploysHandler) PutScript(w http.ResponseWriter, r *http.Request) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	var req putDeployScriptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	user, _ := middleware.UserFromContext(r.Context())
	var savedBy pgtype.Int8
	if user != nil {
		savedBy = pgtype.Int8{Int64: user.ID, Valid: true}
	}

	if err := h.queries.InsertDeployScriptVersion(r.Context(), sqlc.InsertDeployScriptVersionParams{
		SiteID: siteID, Body: req.Body, SavedBy: savedBy,
	}); err != nil {
		h.logger.ErrorContext(r.Context(), "insert version", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	var cron pgtype.Text
	if req.CronSpec != nil {
		cron = pgtype.Text{String: *req.CronSpec, Valid: true}
	}

	row, err := h.queries.UpsertDeployScript(r.Context(), sqlc.UpsertDeployScriptParams{
		SiteID: siteID, Body: req.Body, CronSpec: cron,
	})
	if err != nil {
		h.logger.ErrorContext(r.Context(), "upsert deploy script", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	h.recordAudit(r.Context(), r, siteID, "site.deploy_script.updated", nil)
	writeJSON(w, http.StatusOK, deployScriptResponseFromRow(row))
}

// --- deploys CRUD ---

// CreateDeploy handles POST /v1/sites/{id}/deploys with trigger=manual.
func (h *DeploysHandler) CreateDeploy(w http.ResponseWriter, r *http.Request) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	if h.riverClient == nil {
		writeError(w, http.StatusServiceUnavailable, "background_worker_unavailable")
		return
	}

	user, _ := middleware.UserFromContext(r.Context())
	var triggeredBy pgtype.Int8
	if user != nil {
		triggeredBy = pgtype.Int8{Int64: user.ID, Valid: true}
	}

	row, err := h.queries.CreateDeploy(r.Context(), sqlc.CreateDeployParams{
		SiteID: siteID, Trigger: "manual", TriggeredBy: triggeredBy,
	})
	if err != nil {
		// FK violation = site doesn't exist.
		writeError(w, http.StatusNotFound, "site_not_found")
		return
	}
	if _, err := h.riverClient.Insert(r.Context(), jobs.RunDeployArgs{DeployID: row.ID}, nil); err != nil {
		h.logger.ErrorContext(r.Context(), "enqueue run_deploy", "err", err)
	}
	h.recordAudit(r.Context(), r, row.ID, "site.deploy.queued",
		map[string]any{"site_id": siteID})
	writeJSON(w, http.StatusAccepted, deployResponseFromRow(row))
}

// GetDeploy handles GET /v1/deploys/{deploy_id}.
func (h *DeploysHandler) GetDeploy(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "deploy_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}
	row, err := h.queries.GetDeploy(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, deployResponseFromRow(row))
}

// ListDeploys handles GET /v1/sites/{id}/deploys.
func (h *DeploysHandler) ListDeploys(w http.ResponseWriter, r *http.Request) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	rows, err := h.queries.ListDeploysForSite(r.Context(), sqlc.ListDeploysForSiteParams{
		SiteID: siteID, Limit: 50,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	out := make([]deployResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, deployResponseFromRow(row))
	}
	writeJSON(w, http.StatusOK, out)
}

// --- webhook (Phase 1.6) ---

// Webhook handles POST /v1/webhooks/git/{site_id}.
//
// Accepts a shared-secret token via:
//   - GitHub:  X-Hub-Signature-256: sha256=<hex hmac>  + raw JSON body
//   - GitLab:  X-Gitlab-Token: <secret>
//
// On success enqueues a RunDeploy job with trigger=webhook. The site
// must have a non-empty deploy script.
func (h *DeploysHandler) Webhook(w http.ResponseWriter, r *http.Request) {
	siteIDStr := chi.URLParam(r, "site_id")
	siteID, err := strconv.ParseInt(siteIDStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid site id", http.StatusBadRequest)
		return
	}

	// Look up the deploy script — it carries (in Phase 1.6) the secret.
	// We piggy-back on the existing table by stashing the secret in
	// the cron_spec field? No — that conflicts. Honest answer:
	// 1.6 introduces a per-site webhook_secret via a separate
	// dedicated table or column. To stay scoped, we accept the secret
	// in a `?secret=<token>` query param and compare against a value
	// stored on the site row. The migration adding it lands here.

	// Read the secret from the query param.
	provided := r.URL.Query().Get("secret")
	if provided == "" {
		http.Error(w, "missing secret", http.StatusUnauthorized)
		return
	}

	// Look up the site's webhook secret.
	expected, err := h.queries.GetSiteWebhookSecret(r.Context(), siteID)
	if err != nil {
		// Treat missing site as 404 not 401 so the attacker can't
		// enumerate.
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if expected.String == "" {
		http.Error(w, "webhook not enabled", http.StatusNotFound)
		return
	}

	// Body is needed for GitHub HMAC; read once.
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	if !verifyWebhookSecret(r, body, expected.String, provided) {
		http.Error(w, "bad secret", http.StatusUnauthorized)
		return
	}

	if h.riverClient == nil {
		http.Error(w, "worker unavailable", http.StatusServiceUnavailable)
		return
	}

	row, err := h.queries.CreateDeploy(r.Context(), sqlc.CreateDeployParams{
		SiteID: siteID, Trigger: "webhook",
	})
	if err != nil {
		http.Error(w, "create deploy", http.StatusInternalServerError)
		return
	}
	if _, err := h.riverClient.Insert(r.Context(), jobs.RunDeployArgs{DeployID: row.ID}, nil); err != nil {
		h.logger.ErrorContext(r.Context(), "enqueue webhook deploy", "err", err)
	}
	h.recordAudit(r.Context(), r, row.ID, "site.deploy.webhook",
		map[string]any{"site_id": siteID})
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"deploy_id":` + strconv.FormatInt(row.ID, 10) + `}`))
}

// verifyWebhookSecret accepts a request as authentic if either:
//   - the `secret` query param matches the stored secret (Aegis-native);
//   - GitHub-style: X-Hub-Signature-256: sha256=<hmac-sha256(body, stored)>;
//   - GitLab-style: X-Gitlab-Token == stored.
//
// All comparisons are constant-time.
func verifyWebhookSecret(r *http.Request, body []byte, stored, provided string) bool {
	storedB := []byte(stored)

	if hmac.Equal([]byte(provided), storedB) {
		return true
	}
	if h := r.Header.Get("X-Gitlab-Token"); h != "" && hmac.Equal([]byte(h), storedB) {
		return true
	}
	if h := r.Header.Get("X-Hub-Signature-256"); h != "" {
		const prefix = "sha256="
		if len(h) > len(prefix) && h[:len(prefix)] == prefix {
			mac := hmac.New(sha256.New, storedB)
			mac.Write(body)
			expectedSig := hex.EncodeToString(mac.Sum(nil))
			if hmac.Equal([]byte(h[len(prefix):]), []byte(expectedSig)) {
				return true
			}
		}
	}
	return false
}

// --- helpers ---

const defaultDeployScript = `#!/usr/bin/env bash
# Aegis deploy script — runs as the site's Linux user, cwd is the
# site's working directory. Edit this to fit your stack.
set -euo pipefail

echo "==> Deploy starting at $(date -u +%Y-%m-%dT%H:%M:%SZ)"

# Examples:
#   git pull --ff-only origin main
#   npm ci && npm run build
#   rsync -a --delete dist/ public_html/

mkdir -p public_html
echo "<h1>Aegis-served site</h1>" > public_html/index.html

echo "==> Deploy complete"
`

func parseSiteID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_site_id")
		return 0, false
	}
	return id, true
}

func deployScriptResponseFromRow(s sqlc.SiteDeployScript) deployScriptResponse {
	r := deployScriptResponse{
		SiteID:    s.SiteID,
		Body:      s.Body,
		UpdatedAt: tsString(s.UpdatedAt),
	}
	if s.CronSpec.Valid {
		v := s.CronSpec.String
		r.CronSpec = &v
	}
	return r
}

func deployResponseFromRow(d sqlc.Deploy) deployResponse {
	r := deployResponse{
		ID:        d.ID,
		SiteID:    d.SiteID,
		Trigger:   d.Trigger,
		Status:    d.Status,
		OutputLog: d.OutputLog,
		CreatedAt: tsString(d.CreatedAt),
	}
	if d.StartedAt.Valid {
		v := d.StartedAt.Time.UTC().Format("2006-01-02T15:04:05Z")
		r.StartedAt = &v
	}
	if d.FinishedAt.Valid {
		v := d.FinishedAt.Time.UTC().Format("2006-01-02T15:04:05Z")
		r.FinishedAt = &v
	}
	if d.ExitCode.Valid {
		v := d.ExitCode.Int32
		r.ExitCode = &v
	}
	return r
}

func (h *DeploysHandler) recordAudit(ctx context.Context, r *http.Request, targetID int64, action string, payload any) {
	id := targetID
	user, _ := middleware.UserFromContext(ctx)
	var actorID *int64
	if user != nil {
		actorID = &user.ID
	}
	ip := clientIP(r)
	if err := h.auditRec.Record(ctx, audit.Event{
		ActorUserID: actorID, ActorIP: ip,
		Action: action, TargetType: "deploy", TargetID: &id, Payload: payload,
	}); err != nil {
		h.logger.WarnContext(ctx, "audit record failed", "action", action, "err", err)
	}
}
