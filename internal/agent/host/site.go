// SPDX-License-Identifier: AGPL-3.0-or-later

// Package host implements the agent-side handlers for controller-
// initiated host-management RPCs. Each handler is a thin wrapper
// around a sudoers-listed helper script — argv only, no shell
// metacharacters reach the system.
package host

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"time"

	"github.com/danialrp/aegis/pkg/protocol"
)

const (
	helperDir     = "/usr/local/lib/aegis"
	helperTimeout = 30 * time.Second
)

// Manager owns the host.* RPC handlers. The methods on this type have
// the signature expected by the agent's rpc package, so cmd/agent
// can wire them up directly.
type Manager struct {
	logger *slog.Logger
}

// New builds a Manager.
func New(logger *slog.Logger) *Manager {
	return &Manager{logger: logger}
}

// HandleSiteUserCreate handles host.site_user_create.
func (m *Manager) HandleSiteUserCreate(ctx context.Context, params json.RawMessage) (any, error) {
	return m.runHelper(ctx, "site_useradd", params)
}

// HandleSiteDirEnsure handles host.site_dir_ensure.
func (m *Manager) HandleSiteDirEnsure(ctx context.Context, params json.RawMessage) (any, error) {
	return m.runHelper(ctx, "site_dirsetup", params)
}

// HandleSiteDelete handles host.site_delete.
func (m *Manager) HandleSiteDelete(ctx context.Context, params json.RawMessage) (any, error) {
	return m.runHelper(ctx, "site_delete", params)
}

// runHelper executes `sudo -n <helperDir>/<helper> <site_id>`. The
// helper does its own input validation; we still bounds-check here so
// a typo in a request doesn't end up exec'd.
func (m *Manager) runHelper(ctx context.Context, helper string, params json.RawMessage) (any, error) {
	var p protocol.SiteIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if p.SiteID < 1 || p.SiteID > 1_000_000 {
		return nil, errors.New("site_id out of range")
	}
	ctx, cancel := context.WithTimeout(ctx, helperTimeout)
	defer cancel()

	helperPath := helperDir + "/" + helper
	siteIDStr := strconv.FormatInt(p.SiteID, 10)

	//nolint:gosec // arguments are constrained: helperPath is a constant
	// composed of constants; siteIDStr is %d-formatted from an int64
	// validated above. No shell, no expansion.
	cmd := exec.CommandContext(ctx, "sudo", "-n", "--", helperPath, siteIDStr)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		m.logger.WarnContext(ctx, "helper failed",
			"helper", helper, "site_id", p.SiteID, "err", err, "stderr", stderr.String())
		return nil, fmt.Errorf("helper %s failed: %w (stderr: %s)", helper, err, stderr.String())
	}

	m.logger.InfoContext(ctx, "helper ok", "helper", helper, "site_id", p.SiteID)
	return protocol.HostOKResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}, nil
}
