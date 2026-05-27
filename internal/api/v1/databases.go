// SPDX-License-Identifier: AGPL-3.0-or-later

package v1

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/danialrp/aegis/internal/agentbus"
	"github.com/danialrp/aegis/internal/api/middleware"
	"github.com/danialrp/aegis/internal/audit"
	"github.com/danialrp/aegis/internal/db/sqlc"
	"github.com/danialrp/aegis/pkg/protocol"
)

// DatabasesHandler serves /v1/sites/{id}/databases/*.
type DatabasesHandler struct {
	queries  *sqlc.Queries
	auditRec *audit.Recorder
	hub      *agentbus.Hub
	logger   *slog.Logger
}

// NewDatabasesHandler builds the handler.
func NewDatabasesHandler(q *sqlc.Queries, a *audit.Recorder, hub *agentbus.Hub, logger *slog.Logger) *DatabasesHandler {
	return &DatabasesHandler{queries: q, auditRec: a, hub: hub, logger: logger}
}

// --- shapes ---

type createDatabaseRequest struct {
	Engine   string `json:"engine"`
	Name     string `json:"name"`
	Username string `json:"username"`
	// Password optional — if empty, we generate one.
	Password string `json:"password,omitempty"`
}

type databaseResponse struct {
	ID           int64   `json:"id"`
	SiteID       int64   `json:"site_id"`
	Engine       string  `json:"engine"`
	Name         string  `json:"name"`
	Username     string  `json:"username"`
	Password     string  `json:"password"`
	Status       string  `json:"status"`
	LastError    *string `json:"last_error,omitempty"`
	LastBackupAt *string `json:"last_backup_at,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

type backupRow struct {
	Basename string `json:"basename"`
	Size     int64  `json:"size"`
	ModUnix  int64  `json:"mtime"`
}

type restoreRequest struct {
	Basename string `json:"basename"`
}

// --- handlers ---

// List handles GET /v1/sites/{id}/databases.
func (h *DatabasesHandler) List(w http.ResponseWriter, r *http.Request) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	rows, err := h.queries.ListSiteDatabasesForSite(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	out := make([]databaseResponse, 0, len(rows))
	for _, d := range rows {
		out = append(out, dbResponseFromRow(d))
	}
	writeJSON(w, http.StatusOK, out)
}

// Create handles POST /v1/sites/{id}/databases.
// If Password is empty, a random one is generated and returned.
// Synchronous: the helper RPC runs inline. Creation is fast (<200ms).
func (h *DatabasesHandler) Create(w http.ResponseWriter, r *http.Request) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	var req createDatabaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := validateDBRequest(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Password == "" {
		p, err := randomPassword(18)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		req.Password = p
	}

	site, err := h.queries.GetSite(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusNotFound, "site_not_found")
		return
	}

	row, err := h.queries.CreateSiteDatabase(r.Context(), sqlc.CreateSiteDatabaseParams{
		SiteID: siteID, Engine: req.Engine,
		Name: req.Name, Username: req.Username, Password: req.Password,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "name_already_used")
			return
		}
		h.logger.ErrorContext(r.Context(), "create db row", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Synchronous call to the agent. Failures keep the row but flip
	// status=error so the operator can see + retry by deleting.
	conn, ok := h.hub.Get(site.ServerID)
	if !ok {
		_ = h.queries.SetSiteDatabaseStatus(r.Context(), sqlc.SetSiteDatabaseStatusParams{
			ID: row.ID, Status: "error",
			LastError: pgtype.Text{String: "no live agent", Valid: true},
		})
		writeError(w, http.StatusServiceUnavailable, "agent_offline")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	method := protocol.MethodHostMysqlDBCreate
	if req.Engine == "postgres" {
		method = protocol.MethodHostPostgresDBCreate
	}
	if _, err := conn.Request(ctx, method, protocol.DBCreateParams{
		SiteID:   siteID,
		Engine:   req.Engine,
		Name:     req.Name,
		Username: req.Username,
		Password: req.Password,
	}); err != nil {
		_ = h.queries.SetSiteDatabaseStatus(r.Context(), sqlc.SetSiteDatabaseStatusParams{
			ID: row.ID, Status: "error",
			LastError: pgtype.Text{String: err.Error(), Valid: true},
		})
		updated, _ := h.queries.GetSiteDatabase(r.Context(), row.ID)
		writeJSON(w, http.StatusAccepted, dbResponseFromRow(updated))
		return
	}
	_ = h.queries.SetSiteDatabaseStatus(r.Context(), sqlc.SetSiteDatabaseStatusParams{
		ID: row.ID, Status: "ready",
	})
	updated, _ := h.queries.GetSiteDatabase(r.Context(), row.ID)
	h.recordAudit(r.Context(), r, row.ID, "site.database.created",
		map[string]any{"engine": req.Engine, "name": req.Name})
	writeJSON(w, http.StatusCreated, dbResponseFromRow(updated))
}

// Delete handles DELETE /v1/sites/{id}/databases/{db_id}.
func (h *DatabasesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	dbID, err := strconv.ParseInt(chi.URLParam(r, "db_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}
	d, site, err := h.fetchDBAndSite(r.Context(), dbID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	// Best-effort host drop.
	if conn, ok := h.hub.Get(site.ServerID); ok {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		method := protocol.MethodHostMysqlDBDrop
		if d.Engine == "postgres" {
			method = protocol.MethodHostPostgresDBDrop
		}
		_, _ = conn.Request(ctx, method, protocol.DBDropParams{
			Engine: d.Engine, Name: d.Name, Username: d.Username,
		})
		cancel()
	}
	if err := h.queries.DeleteSiteDatabase(r.Context(), dbID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	h.recordAudit(r.Context(), r, dbID, "site.database.deleted", nil)
	w.WriteHeader(http.StatusNoContent)
}

// Backup handles POST /v1/sites/{id}/databases/{db_id}/backup.
func (h *DatabasesHandler) Backup(w http.ResponseWriter, r *http.Request) {
	dbID, err := strconv.ParseInt(chi.URLParam(r, "db_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}
	d, site, err := h.fetchDBAndSite(r.Context(), dbID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	conn, ok := h.hub.Get(site.ServerID)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "agent_offline")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	resp, err := conn.Request(ctx, protocol.MethodHostDBBackup, protocol.DBBackupParams{
		SiteID: site.ID, Engine: d.Engine, Name: d.Name,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "rpc_failed")
		return
	}
	var result protocol.DBBackupResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		writeError(w, http.StatusInternalServerError, "decode_failed")
		return
	}
	_ = h.queries.TouchSiteDatabaseBackup(r.Context(), dbID)
	h.recordAudit(r.Context(), r, dbID, "site.database.backed_up",
		map[string]any{"path": result.Path})
	writeJSON(w, http.StatusOK, map[string]string{"path": result.Path})
}

// ListBackups handles GET /v1/sites/{id}/databases/{db_id}/backups.
// Filters to backups for this database's engine + name.
func (h *DatabasesHandler) ListBackups(w http.ResponseWriter, r *http.Request) {
	dbID, err := strconv.ParseInt(chi.URLParam(r, "db_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}
	d, site, err := h.fetchDBAndSite(r.Context(), dbID)
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
	resp, err := conn.Request(ctx, protocol.MethodHostDBBackupsList, protocol.SiteIDParams{SiteID: site.ID})
	if err != nil {
		writeError(w, http.StatusBadGateway, "rpc_failed")
		return
	}
	var result protocol.DBBackupsListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		writeError(w, http.StatusInternalServerError, "decode_failed")
		return
	}
	prefix := d.Engine + "-" + d.Name + "-"
	out := make([]backupRow, 0)
	for _, line := range strings.Split(result.Raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		if !strings.HasPrefix(parts[0], prefix) {
			continue
		}
		size, _ := strconv.ParseInt(parts[1], 10, 64)
		mt, _ := strconv.ParseFloat(parts[2], 64)
		out = append(out, backupRow{
			Basename: parts[0],
			Size:     size,
			ModUnix:  int64(mt),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// Restore handles POST /v1/sites/{id}/databases/{db_id}/restore.
func (h *DatabasesHandler) Restore(w http.ResponseWriter, r *http.Request) {
	dbID, err := strconv.ParseInt(chi.URLParam(r, "db_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}
	var req restoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Basename == "" {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	d, site, err := h.fetchDBAndSite(r.Context(), dbID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	conn, ok := h.hub.Get(site.ServerID)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "agent_offline")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	if _, err := conn.Request(ctx, protocol.MethodHostDBRestore, protocol.DBRestoreParams{
		SiteID: site.ID, Engine: d.Engine, Name: d.Name, Basename: req.Basename,
	}); err != nil {
		writeError(w, http.StatusBadGateway, "rpc_failed")
		return
	}
	h.recordAudit(r.Context(), r, dbID, "site.database.restored",
		map[string]any{"basename": req.Basename})
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func (h *DatabasesHandler) fetchDBAndSite(ctx context.Context, dbID int64) (sqlc.SiteDatabase, sqlc.Site, error) {
	d, err := h.queries.GetSiteDatabase(ctx, dbID)
	if err != nil {
		return sqlc.SiteDatabase{}, sqlc.Site{}, err
	}
	site, err := h.queries.GetSite(ctx, d.SiteID)
	return d, site, err
}

func validateDBRequest(req *createDatabaseRequest) error {
	switch req.Engine {
	case "mysql", "postgres":
	default:
		return errors.New("invalid_engine")
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Username = strings.TrimSpace(req.Username)
	if req.Name == "" || !isAlnumUnder(req.Name) || len(req.Name) > 63 {
		return errors.New("invalid_name")
	}
	if req.Username == "" || !isAlnumUnder(req.Username) || len(req.Username) > 32 {
		return errors.New("invalid_username")
	}
	if req.Password != "" && len(req.Password) > 256 {
		return errors.New("password_too_long")
	}
	return nil
}

func isAlnumUnder(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_':
		default:
			return false
		}
	}
	return true
}

func randomPassword(bytes int) (string, error) {
	if bytes < 12 {
		bytes = 12
	}
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// URL-safe alphabet keeps the password copy-pasteable without shell
	// escaping concerns. 18 bytes → 24 chars.
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func dbResponseFromRow(d sqlc.SiteDatabase) databaseResponse {
	r := databaseResponse{
		ID:        d.ID,
		SiteID:    d.SiteID,
		Engine:    d.Engine,
		Name:      d.Name,
		Username:  d.Username,
		Password:  d.Password,
		Status:    d.Status,
		CreatedAt: tsString(d.CreatedAt),
	}
	if d.LastError.Valid {
		v := d.LastError.String
		r.LastError = &v
	}
	if d.LastBackupAt.Valid {
		v := tsStringFromPg(d.LastBackupAt)
		r.LastBackupAt = &v
	}
	return r
}

func (h *DatabasesHandler) recordAudit(ctx context.Context, r *http.Request, targetID int64, action string, payload any) {
	id := targetID
	user, _ := middleware.UserFromContext(ctx)
	var actorID *int64
	if user != nil {
		actorID = &user.ID
	}
	ip := clientIP(r)
	if err := h.auditRec.Record(ctx, audit.Event{
		ActorUserID: actorID, ActorIP: ip,
		Action: action, TargetType: "site_database",
		TargetID: &id, Payload: payload,
	}); err != nil {
		h.logger.WarnContext(ctx, "audit record failed", "action", action, "err", err)
	}
}
