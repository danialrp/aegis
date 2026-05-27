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
	"time"

	"github.com/danialrp/aegis/pkg/protocol"
)

// HandleNginxApplyProxyVhost handles host.nginx_apply_proxy_vhost.
func (m *Manager) HandleNginxApplyProxyVhost(ctx context.Context, params json.RawMessage) (any, error) {
	var p protocol.NginxApplyProxyVhostParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if p.SiteID < 1 || p.SiteID > 1_000_000 {
		return nil, errors.New("site_id out of range")
	}
	if !validDomain(p.Domain) {
		return nil, errors.New("invalid domain")
	}
	if p.ProxyPort < 1 || p.ProxyPort > 65535 {
		return nil, errors.New("invalid proxy_port")
	}
	return runVettedHelper(ctx, "nginx_write_proxy_vhost", []string{
		strconv.FormatInt(p.SiteID, 10),
		p.Domain,
		strconv.Itoa(p.ProxyPort),
	})
}

// HandleComposeWrite handles host.compose_write. Body comes in as a
// JSON string param; helper reads from stdin.
func (m *Manager) HandleComposeWrite(ctx context.Context, params json.RawMessage) (any, error) {
	var p protocol.ComposeWriteParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if p.SiteID < 1 || p.SiteID > 1_000_000 {
		return nil, errors.New("site_id out of range")
	}
	const maxComposeBytes = 256 * 1024
	if len(p.Body) > maxComposeBytes {
		return nil, fmt.Errorf("compose body too large (>%d bytes)", maxComposeBytes)
	}

	cctx, cancel := context.WithTimeout(ctx, helperTimeout)
	defer cancel()

	//nolint:gosec // helper path constant; site_id range-checked
	cmd := exec.CommandContext(cctx, "sudo", "-n", "--",
		helperDir+"/compose_write",
		strconv.FormatInt(p.SiteID, 10),
	)
	cmd.Stdin = bytes.NewReader([]byte(p.Body))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("compose_write: %w (stderr: %s)", err, stderr.String())
	}
	return protocol.HostOKResult{Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

// HandleComposeAction handles host.compose_action. Uses the caller's
// deadline directly since pulls + builds can be slow.
func (m *Manager) HandleComposeAction(ctx context.Context, params json.RawMessage) (any, error) {
	var p protocol.ComposeActionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if p.SiteID < 1 || p.SiteID > 1_000_000 {
		return nil, errors.New("site_id out of range")
	}
	switch p.Action {
	case "up", "down", "restart", "pull", "build":
	default:
		return nil, errors.New("invalid action")
	}
	return runVettedHelper(ctx, "compose_action", []string{
		strconv.FormatInt(p.SiteID, 10),
		p.Action,
	})
}

// HandleComposePs handles host.compose_ps.
func (m *Manager) HandleComposePs(ctx context.Context, params json.RawMessage) (any, error) {
	var p protocol.SiteIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if p.SiteID < 1 || p.SiteID > 1_000_000 {
		return nil, errors.New("site_id out of range")
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := runVettedHelper(cctx, "compose_ps", []string{
		strconv.FormatInt(p.SiteID, 10),
	})
	if err != nil {
		return nil, err
	}
	ok := out.(protocol.HostOKResult)
	return protocol.ComposePsResult{Raw: ok.Stdout}, nil
}

// HandleComposeLogs handles host.compose_logs.
func (m *Manager) HandleComposeLogs(ctx context.Context, params json.RawMessage) (any, error) {
	var p protocol.ComposeLogsParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if p.SiteID < 1 || p.SiteID > 1_000_000 {
		return nil, errors.New("site_id out of range")
	}
	if p.Lines <= 0 {
		p.Lines = 200
	}
	if p.Lines > 5000 {
		p.Lines = 5000
	}
	if !validComposeService(p.Service) {
		return nil, errors.New("invalid service name")
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := runVettedHelper(cctx, "compose_logs", []string{
		strconv.FormatInt(p.SiteID, 10),
		p.Service,
		strconv.Itoa(p.Lines),
	})
	if err != nil {
		return nil, err
	}
	ok := out.(protocol.HostOKResult)
	return protocol.ComposeLogsResult{Output: ok.Stdout}, nil
}

// validComposeService allows an empty string (whole project) or a
// docker-compose service name shape.
func validComposeService(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}
