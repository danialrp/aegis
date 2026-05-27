// SPDX-License-Identifier: AGPL-3.0-or-later

package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/danialrp/aegis/internal/agentbus"
	"github.com/danialrp/aegis/internal/audit"
	"github.com/danialrp/aegis/internal/db/sqlc"
	"github.com/danialrp/aegis/pkg/protocol"
)

// ProvisionSiteArgs is the payload of one site-provisioning job.
type ProvisionSiteArgs struct {
	SiteID int64 `json:"site_id"`
}

// Kind is river's discriminator for this job type.
func (ProvisionSiteArgs) Kind() string { return "provision_site" }

// ProvisionSiteWorker runs the host-side steps that bring a site
// from status=pending to status=ready: create the site_<id> Linux
// user and lay out its working directory. Phase 1.3 will extend it
// with nginx vhost generation.
type ProvisionSiteWorker struct {
	river.WorkerDefaults[ProvisionSiteArgs]

	Queries *sqlc.Queries
	Audit   *audit.Recorder
	Hub     *agentbus.Hub
	Logger  *slog.Logger
}

// Work is invoked by river.
func (w *ProvisionSiteWorker) Work(ctx context.Context, job *river.Job[ProvisionSiteArgs]) error {
	args := job.Args
	log := w.Logger.With("site_id", args.SiteID)

	site, err := w.Queries.GetSite(ctx, args.SiteID)
	if err != nil {
		return fmt.Errorf("lookup site: %w", err)
	}

	if err := w.Queries.SetSiteStatus(ctx, sqlc.SetSiteStatusParams{
		ID: args.SiteID, ProvisionStatus: "provisioning",
	}); err != nil {
		return fmt.Errorf("mark provisioning: %w", err)
	}
	_ = w.Queries.ClearSiteProvisionError(ctx, args.SiteID)
	w.auditf(ctx, args.SiteID, "site.provision.started", nil)

	if err := w.run(ctx, site, log); err != nil {
		w.recordFailure(ctx, args.SiteID, err, log)
		return nil // don't let river retry: operator-driven
	}

	if err := w.Queries.SetSiteStatus(ctx, sqlc.SetSiteStatusParams{
		ID: args.SiteID, ProvisionStatus: "ready",
	}); err != nil {
		return fmt.Errorf("mark ready: %w", err)
	}
	w.auditf(ctx, args.SiteID, "site.provision.succeeded", nil)
	log.Info("site provisioning complete")
	return nil
}

func (w *ProvisionSiteWorker) run(ctx context.Context, site sqlc.Site, log *slog.Logger) error {
	conn, ok := w.Hub.Get(site.ServerID)
	if !ok {
		return fmt.Errorf("no live agent for server %d", site.ServerID)
	}

	siteIDParams := protocol.SiteIDParams{SiteID: site.ID}

	// User + working dir.
	for _, step := range []struct {
		method string
		desc   string
	}{
		{protocol.MethodHostSiteUserCreate, "create site user"},
		{protocol.MethodHostSiteDirEnsure, "ensure site dir"},
	} {
		log.Info("step", "name", step.desc)
		callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		_, err := conn.Request(callCtx, step.method, siteIDParams)
		cancel()
		if err != nil {
			return fmt.Errorf("%s: %w", step.desc, err)
		}
	}

	// nginx vhost. Only `static` sites get a vhost in Phase 1; php /
	// docker site types land with their own templates in later phases.
	if site.SiteType == "static" {
		log.Info("step", "name", "apply nginx vhost")
		vhostParams := protocol.NginxApplyVhostParams{
			SiteID:     site.ID,
			Domain:     site.Domain,
			WorkingDir: site.WorkingDir,
		}
		callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		_, err := conn.Request(callCtx, protocol.MethodHostNginxApplyVhost, vhostParams)
		cancel()
		if err != nil {
			return fmt.Errorf("nginx apply: %w", err)
		}
	}
	return nil
}

func (w *ProvisionSiteWorker) recordFailure(ctx context.Context, siteID int64, cause error, log *slog.Logger) {
	log.Error("site provisioning failed", "err", cause)
	if err := w.Queries.SetSiteProvisionError(ctx, sqlc.SetSiteProvisionErrorParams{
		ID: siteID, ProvisionError: textOrNull(cause.Error()),
	}); err != nil {
		log.Error("record error failed", "err", err)
	}
	w.auditf(ctx, siteID, "site.provision.failed",
		map[string]string{"error": cause.Error()})
}

func (w *ProvisionSiteWorker) auditf(ctx context.Context, siteID int64, action string, payload any) {
	id := siteID
	if err := w.Audit.Record(ctx, audit.Event{
		Action:     action,
		TargetType: "site",
		TargetID:   &id,
		Payload:    payload,
	}); err != nil {
		w.Logger.Warn("audit record failed", "action", action, "err", err)
	}
}

// (compile-time check that errors is imported — silences linter if no
// other usage remains.)
var _ = errors.New
