// SPDX-License-Identifier: AGPL-3.0-or-later

package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/riverqueue/river"

	"github.com/danialrp/aegis/internal/audit"
	"github.com/danialrp/aegis/internal/ca"
	"github.com/danialrp/aegis/internal/db/sqlc"
	"github.com/danialrp/aegis/internal/provisioner"
)

// textOrNull mirrors the helper in internal/auth — kept local to
// avoid cross-package coupling for a one-line utility.
func textOrNull(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

// ProvisionServerArgs is the payload of one provisioning job.
//
// Note: Password and PrivateKey are intentionally NOT redacted via
// custom marshalling for 0.8 — river stores args in JSON in Postgres
// alongside the rest of the row, which is itself the controller's
// source of truth. If your controller DB is breached, far worse
// secrets are already at risk. The fix (encrypting per-job args
// before insert) is a Phase 7+ task tracked separately.
type ProvisionServerArgs struct {
	ServerID   int64  `json:"server_id"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	Password   string `json:"password,omitempty"`
	PrivateKey []byte `json:"private_key,omitempty"`
}

// Kind is river's discriminator for this job type.
func (ProvisionServerArgs) Kind() string { return "provision_server" }

// AgentBinaryFunc is the signature jobs use to look up cross-compiled
// agent binaries. Decoupled from the agentbinary package so tests can
// supply a fake.
type AgentBinaryFunc func(arch string) ([]byte, error)

// ProvisionWorker runs ProvisionServerArgs jobs.
type ProvisionWorker struct {
	river.WorkerDefaults[ProvisionServerArgs]

	Queries *sqlc.Queries
	Pool    interface {
		// only used for transactional touchpoints; keep the smallest
		// surface so tests can stub it. Empty for now.
	}
	Audit         *audit.Recorder
	CA            *ca.CA
	Provisioner   provisioner.Provisioner
	AgentBinary   AgentBinaryFunc
	ControllerURL string
	WaitTimeout   time.Duration
	Logger        *slog.Logger
}

// Work is invoked by river for each enqueued job.
func (w *ProvisionWorker) Work(ctx context.Context, job *river.Job[ProvisionServerArgs]) error {
	args := job.Args
	log := w.Logger.With("server_id", args.ServerID, "host", args.Host)

	// Transition status: pending → provisioning. Clear any previous error.
	if err := w.Queries.SetServerStatus(ctx, sqlc.SetServerStatusParams{
		ID: args.ServerID, ProvisionStatus: "provisioning",
	}); err != nil {
		return fmt.Errorf("mark provisioning: %w", err)
	}
	if err := w.Queries.ClearServerProvisionError(ctx, args.ServerID); err != nil {
		log.Warn("clear prior error", "err", err)
	}
	w.auditf(ctx, args.ServerID, "server.provision.started", nil)

	if err := w.run(ctx, args, log); err != nil {
		w.recordFailure(ctx, args.ServerID, err, log)
		// Returning nil so river does NOT retry — operator must retry
		// explicitly from the UI. Auto-retry could re-attempt against
		// a half-installed host and make recovery harder.
		return nil
	}

	if err := w.Queries.SetServerStatus(ctx, sqlc.SetServerStatusParams{
		ID: args.ServerID, ProvisionStatus: "ready",
	}); err != nil {
		log.Error("mark ready failed", "err", err)
		return fmt.Errorf("mark ready: %w", err)
	}
	w.auditf(ctx, args.ServerID, "server.provision.succeeded", nil)
	log.Info("provisioning complete")
	return nil
}

func (w *ProvisionWorker) run(ctx context.Context, args ProvisionServerArgs, log *slog.Logger) error {
	bundle, err := w.CA.IssueAgentCert(args.ServerID)
	if err != nil {
		return fmt.Errorf("issue cert: %w", err)
	}
	if err := w.Queries.SetServerAgentFingerprint(ctx, sqlc.SetServerAgentFingerprintParams{
		ID: args.ServerID, AgentFingerprint: textOrNull(bundle.Fingerprint),
	}); err != nil {
		return fmt.Errorf("record fingerprint: %w", err)
	}

	target := provisioner.Target{
		Host:           args.Host,
		Port:           args.Port,
		User:           args.User,
		Password:       args.Password,
		PrivateKey:     args.PrivateKey,
		ServerID:       args.ServerID,
		AgentBinaryFor: w.AgentBinary,
		AgentCertPEM:   bundle.CertPEM,
		AgentKeyPEM:    bundle.KeyPEM,
		ControllerCA:   w.CA.CACertPEM(),
		ControllerURL:  w.ControllerURL,
		Logger:         log,
	}

	if err := w.Provisioner.Provision(ctx, target); err != nil {
		return err
	}

	// Wait for the agent to dial in. The hub's TouchServerAgent updates
	// servers.agent_last_seen on each successful handshake, so polling
	// the row is the cheapest reliable signal we have.
	return w.waitForAgent(ctx, args.ServerID, log)
}

func (w *ProvisionWorker) waitForAgent(ctx context.Context, serverID int64, log *slog.Logger) error {
	deadline := time.Now().Add(w.WaitTimeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		srv, err := w.Queries.GetServer(ctx, serverID)
		if err != nil {
			return fmt.Errorf("read server row: %w", err)
		}
		if srv.AgentLastSeen.Valid &&
			time.Since(srv.AgentLastSeen.Time) < w.WaitTimeout {
			log.Info("agent dialed in", "last_seen", srv.AgentLastSeen.Time)
			return nil
		}

		if time.Now().After(deadline) {
			return errors.New("agent did not dial controller within timeout")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (w *ProvisionWorker) recordFailure(ctx context.Context, serverID int64, cause error, log *slog.Logger) {
	log.Error("provisioning failed", "err", cause)
	if err := w.Queries.SetServerProvisionError(ctx, sqlc.SetServerProvisionErrorParams{
		ID: serverID, ProvisionError: textOrNull(cause.Error()),
	}); err != nil {
		log.Error("record error failed", "err", err)
	}
	w.auditf(ctx, serverID, "server.provision.failed",
		map[string]string{"error": cause.Error()})
}

func (w *ProvisionWorker) auditf(ctx context.Context, serverID int64, action string, payload any) {
	id := serverID
	if err := w.Audit.Record(ctx, audit.Event{
		Action:     action,
		TargetType: "server",
		TargetID:   &id,
		Payload:    payload,
	}); err != nil {
		w.Logger.Warn("audit record failed", "action", action, "err", err)
	}
}
