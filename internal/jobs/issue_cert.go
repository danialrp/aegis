// SPDX-License-Identifier: AGPL-3.0-or-later

package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/riverqueue/river"

	"github.com/danialrp/aegis/internal/agentbus"
	"github.com/danialrp/aegis/internal/audit"
	"github.com/danialrp/aegis/internal/db/sqlc"
	"github.com/danialrp/aegis/pkg/protocol"
)

// IssueCertArgs is the payload of one cert-issuance job.
type IssueCertArgs struct {
	CertID int64  `json:"cert_id"`
	Email  string `json:"email"`
}

// Kind is river's discriminator.
func (IssueCertArgs) Kind() string { return "issue_cert" }

// IssueCertWorker invokes host.cert_issue on the right agent, polls
// host.cert_status until certbot reports the new cert, parses its
// expiry, and persists everything on the site_certs row.
type IssueCertWorker struct {
	river.WorkerDefaults[IssueCertArgs]

	Queries *sqlc.Queries
	Audit   *audit.Recorder
	Hub     *agentbus.Hub
	Logger  *slog.Logger
}

// Work runs one issuance.
func (w *IssueCertWorker) Work(ctx context.Context, job *river.Job[IssueCertArgs]) error {
	cert, err := w.Queries.GetSiteCert(ctx, job.Args.CertID)
	if err != nil {
		return fmt.Errorf("lookup cert: %w", err)
	}
	site, err := w.Queries.GetSite(ctx, cert.SiteID)
	if err != nil {
		return fmt.Errorf("lookup site: %w", err)
	}
	log := w.Logger.With("cert_id", cert.ID, "site_id", site.ID, "domain", cert.Domain)

	if err := w.Queries.SetSiteCertStatus(ctx, sqlc.SetSiteCertStatusParams{
		ID: cert.ID, Status: "issuing",
	}); err != nil {
		return fmt.Errorf("mark issuing: %w", err)
	}
	w.auditf(ctx, cert.ID, "site.cert.issuing", map[string]any{
		"domain": cert.Domain,
	})

	if err := w.run(ctx, site.ServerID, cert.Domain, job.Args.Email, log); err != nil {
		log.Error("cert issuance failed", "err", err)
		_ = w.Queries.SetSiteCertError(ctx, sqlc.SetSiteCertErrorParams{
			ID: cert.ID, LastError: pgtype.Text{String: err.Error(), Valid: true},
		})
		w.auditf(ctx, cert.ID, "site.cert.failed", map[string]any{
			"error": err.Error(),
		})
		return nil
	}

	// Read back expiry from certbot.
	expires, err := w.readExpiry(ctx, site.ServerID, cert.Domain)
	if err != nil {
		log.Warn("expiry read failed; cert is issued but expiry unknown", "err", err)
	}
	expPg := pgtype.Timestamptz{}
	if !expires.IsZero() {
		expPg = pgtype.Timestamptz{Time: expires, Valid: true}
	}
	if err := w.Queries.SetSiteCertIssued(ctx, sqlc.SetSiteCertIssuedParams{
		ID: cert.ID, ExpiresAt: expPg,
	}); err != nil {
		return fmt.Errorf("mark issued: %w", err)
	}
	w.auditf(ctx, cert.ID, "site.cert.issued", map[string]any{
		"domain":     cert.Domain,
		"expires_at": expires.Format(time.RFC3339),
	})
	log.Info("cert issued", "expires_at", expires)
	return nil
}

func (w *IssueCertWorker) run(ctx context.Context, serverID int64, domain, email string, log *slog.Logger) error {
	conn, ok := w.Hub.Get(serverID)
	if !ok {
		return fmt.Errorf("no live agent for server %d", serverID)
	}
	log.Info("calling host.cert_issue")
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	_, err := conn.Request(callCtx, protocol.MethodHostCertIssue, protocol.CertIssueParams{
		Domain: domain, Email: email,
	})
	return err
}

// readExpiry calls host.cert_status and pulls the matching domain's
// `Expiry Date` out of certbot's plain-text output.
func (w *IssueCertWorker) readExpiry(ctx context.Context, serverID int64, domain string) (time.Time, error) {
	conn, ok := w.Hub.Get(serverID)
	if !ok {
		return time.Time{}, fmt.Errorf("no live agent for server %d", serverID)
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := conn.Request(callCtx, protocol.MethodHostCertStatus, nil)
	if err != nil {
		return time.Time{}, err
	}
	var result protocol.CertStatusResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return time.Time{}, fmt.Errorf("decode cert status: %w", err)
	}
	return parseCertbotExpiry(result.Raw, domain)
}

// certbotCertRE matches a single block in `certbot certificates`:
//
//	Certificate Name: example.com
//	  Domains: example.com www.example.com
//	  Expiry Date: 2026-08-25 12:34:56+00:00 (VALID: 89 days)
//
// We grab the Expiry Date line for the block whose Certificate Name
// equals the requested domain (certbot's default name).
var certbotCertRE = regexp.MustCompile(
	`Certificate Name:\s*(\S+)\s*\n[\s\S]*?Expiry Date:\s*(\S+\s\S+)`,
)

// parseCertbotExpiry returns the first matching expiry for domain.
func parseCertbotExpiry(raw, domain string) (time.Time, error) {
	for _, m := range certbotCertRE.FindAllStringSubmatch(raw, -1) {
		if !strings.EqualFold(strings.TrimSpace(m[1]), domain) {
			continue
		}
		// certbot uses "2026-08-25 12:34:56+00:00".
		t, err := time.Parse("2006-01-02 15:04:05-07:00", m[2])
		if err != nil {
			return time.Time{}, fmt.Errorf("parse expiry %q: %w", m[2], err)
		}
		return t, nil
	}
	return time.Time{}, errors.New("expiry not found in certbot output")
}

func (w *IssueCertWorker) auditf(ctx context.Context, certID int64, action string, payload any) {
	id := certID
	if err := w.Audit.Record(ctx, audit.Event{
		Action:     action,
		TargetType: "site_cert",
		TargetID:   &id,
		Payload:    payload,
	}); err != nil {
		w.Logger.Warn("audit record failed", "action", action, "err", err)
	}
}
