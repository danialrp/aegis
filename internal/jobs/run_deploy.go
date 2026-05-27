// SPDX-License-Identifier: AGPL-3.0-or-later

package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/riverqueue/river"

	"github.com/danialrp/aegis/internal/agentbus"
	"github.com/danialrp/aegis/internal/audit"
	"github.com/danialrp/aegis/internal/db/sqlc"
	"github.com/danialrp/aegis/pkg/protocol"
)

// RunDeployArgs is the payload of one deploy job.
type RunDeployArgs struct {
	DeployID int64 `json:"deploy_id"`
}

// Kind is river's discriminator for this job type.
func (RunDeployArgs) Kind() string { return "run_deploy" }

// RunDeployWorker invokes host.site_run_script on the right agent,
// records the resulting output + exit code, and transitions the
// deploys row accordingly.
type RunDeployWorker struct {
	river.WorkerDefaults[RunDeployArgs]

	Queries *sqlc.Queries
	Audit   *audit.Recorder
	Hub     *agentbus.Hub
	Logger  *slog.Logger

	// DeployTimeout caps how long a deploy may run. Long-running
	// deploys can blow past this — operators set it via env. Defaults
	// to 30m, which is comfortably beyond Forge's 10m timeout we set
	// out to beat.
	DeployTimeout time.Duration
}

// Work runs one deploy.
func (w *RunDeployWorker) Work(ctx context.Context, job *river.Job[RunDeployArgs]) error {
	deploy, err := w.Queries.GetDeploy(ctx, job.Args.DeployID)
	if err != nil {
		return fmt.Errorf("lookup deploy: %w", err)
	}
	site, err := w.Queries.GetSite(ctx, deploy.SiteID)
	if err != nil {
		return fmt.Errorf("lookup site: %w", err)
	}
	script, err := w.Queries.GetDeployScript(ctx, deploy.SiteID)
	if err != nil {
		return fmt.Errorf("lookup deploy script: %w", err)
	}
	if script.Body == "" {
		return w.fail(ctx, deploy.ID, "deploy script is empty", -1)
	}

	if err := w.Queries.MarkDeployRunning(ctx, deploy.ID); err != nil {
		return fmt.Errorf("mark running: %w", err)
	}
	w.auditf(ctx, deploy.ID, "site.deploy.started", map[string]any{
		"site_id": site.ID, "trigger": deploy.Trigger,
	})

	conn, ok := w.Hub.Get(site.ServerID)
	if !ok {
		return w.fail(ctx, deploy.ID, fmt.Sprintf("no live agent for server %d", site.ServerID), -1)
	}

	timeout := w.DeployTimeout
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := conn.Request(callCtx, protocol.MethodHostSiteRunScript, protocol.RunScriptParams{
		SiteID:     site.ID,
		ScriptBody: script.Body,
	})
	if err != nil {
		return w.fail(ctx, deploy.ID, fmt.Sprintf("run script rpc: %v", err), -1)
	}

	var result protocol.RunScriptResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return w.fail(ctx, deploy.ID, fmt.Sprintf("decode result: %v", err), -1)
	}

	// Persist the captured output. AppendDeployOutput is a single
	// concat — fine for the typical few-KB case. Larger outputs land
	// when sites grow; we can roll to per-line later.
	if result.Output != "" {
		if err := w.Queries.AppendDeployOutput(ctx, sqlc.AppendDeployOutputParams{
			ID: deploy.ID, OutputLog: result.Output,
		}); err != nil {
			w.Logger.Warn("persist output failed", "deploy_id", deploy.ID, "err", err)
		}
	}

	status := "succeeded"
	if result.ExitCode != 0 {
		status = "failed"
	}
	if err := w.Queries.MarkDeployFinished(ctx, sqlc.MarkDeployFinishedParams{
		ID:       deploy.ID,
		Status:   status,
		ExitCode: pgtype.Int4{Int32: clampInt32(result.ExitCode), Valid: true},
	}); err != nil {
		return fmt.Errorf("mark finished: %w", err)
	}
	w.auditf(ctx, deploy.ID, "site.deploy."+status, map[string]any{
		"exit_code": result.ExitCode,
	})
	return nil
}

func (w *RunDeployWorker) fail(ctx context.Context, deployID int64, msg string, exit int) error {
	w.Logger.Error("deploy failed", "deploy_id", deployID, "msg", msg)
	_ = w.Queries.AppendDeployOutput(ctx, sqlc.AppendDeployOutputParams{
		ID: deployID, OutputLog: "aegis: " + msg + "\n",
	})
	if err := w.Queries.MarkDeployFinished(ctx, sqlc.MarkDeployFinishedParams{
		ID:       deployID,
		Status:   "failed",
		ExitCode: pgtype.Int4{Int32: clampInt32(exit), Valid: true},
	}); err != nil {
		w.Logger.Error("mark failed", "err", err)
	}
	w.auditf(ctx, deployID, "site.deploy.failed",
		map[string]string{"reason": msg})
	// Returning nil so river does not retry — operator decides.
	return nil
}

func (w *RunDeployWorker) auditf(ctx context.Context, deployID int64, action string, payload any) {
	id := deployID
	if err := w.Audit.Record(ctx, audit.Event{
		Action:     action,
		TargetType: "deploy",
		TargetID:   &id,
		Payload:    payload,
	}); err != nil {
		w.Logger.Warn("audit record failed", "action", action, "err", err)
	}
}

// clampInt32 saturates v into the int32 range so a misbehaving
// helper that returns exit codes outside [-2^31, 2^31) cannot blow
// past gosec G115. POSIX exit codes are 8-bit anyway; this is
// defense in depth.
func clampInt32(v int) int32 {
	switch {
	case v > 2147483647:
		return 2147483647
	case v < -2147483648:
		return -2147483648
	default:
		return int32(v)
	}
}

var _ = errors.New // keep imports honest if all uses get refactored away
