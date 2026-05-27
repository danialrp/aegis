// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"

	"github.com/danialrp/aegis/pkg/protocol"
)

// HandleNginxApplyVhost handles host.nginx_apply_vhost.
//
// All three args are passed through `sudo --` to the helper, which
// does its own strict validation (numeric site id, domain charset
// allow-list, working_dir = /srv/sites/<id>).
func (m *Manager) HandleNginxApplyVhost(ctx context.Context, params json.RawMessage) (any, error) {
	var p protocol.NginxApplyVhostParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if p.SiteID < 1 || p.SiteID > 1_000_000 {
		return nil, errors.New("site_id out of range")
	}
	if p.Domain == "" {
		return nil, errors.New("domain required")
	}
	if p.WorkingDir == "" {
		return nil, errors.New("working_dir required")
	}

	ctx, cancel := context.WithTimeout(ctx, helperTimeout)
	defer cancel()

	//nolint:gosec // args are validated above; helper additionally re-validates.
	cmd := exec.CommandContext(ctx,
		"sudo", "-n", "--",
		helperDir+"/nginx_write_vhost",
		strconv.FormatInt(p.SiteID, 10),
		p.Domain,
		p.WorkingDir,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("nginx_write_vhost: %w (stderr: %s)", err, stderr.String())
	}
	return protocol.HostOKResult{Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

// HandleNginxRemoveVhost handles host.nginx_remove_vhost.
func (m *Manager) HandleNginxRemoveVhost(ctx context.Context, params json.RawMessage) (any, error) {
	return m.runHelper(ctx, "nginx_remove_vhost", params)
}
