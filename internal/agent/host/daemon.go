// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/danialrp/aegis/pkg/protocol"
)

// HandleDaemonWrite handles host.daemon_write.
func (m *Manager) HandleDaemonWrite(ctx context.Context, params json.RawMessage) (any, error) {
	var p protocol.DaemonWriteParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if err := validateDaemon(p.SiteID, p.Slug); err != nil {
		return nil, err
	}
	if p.Command == "" {
		return nil, errors.New("command required")
	}
	autoRestart := "false"
	if p.AutoRestart {
		autoRestart = "true"
	}
	return runVettedHelper(ctx, "daemon_write", []string{
		strconv.FormatInt(p.SiteID, 10),
		p.Slug,
		p.Command,
		autoRestart,
	})
}

// HandleDaemonRemove handles host.daemon_remove.
func (m *Manager) HandleDaemonRemove(ctx context.Context, params json.RawMessage) (any, error) {
	var p protocol.DaemonSlugParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if err := validateDaemon(p.SiteID, p.Slug); err != nil {
		return nil, err
	}
	return runVettedHelper(ctx, "daemon_remove", []string{
		strconv.FormatInt(p.SiteID, 10),
		p.Slug,
	})
}

// HandleDaemonAction handles host.daemon_action — start / stop / restart.
func (m *Manager) HandleDaemonAction(ctx context.Context, params json.RawMessage) (any, error) {
	var p protocol.DaemonActionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if err := validateDaemon(p.SiteID, p.Slug); err != nil {
		return nil, err
	}
	switch p.Action {
	case "start", "stop", "restart":
	default:
		return nil, errors.New("action must be start|stop|restart")
	}
	return runVettedHelper(ctx, "daemon_action", []string{
		strconv.FormatInt(p.SiteID, 10),
		p.Slug,
		p.Action,
	})
}

// HandleDaemonLogs handles host.daemon_logs.
func (m *Manager) HandleDaemonLogs(ctx context.Context, params json.RawMessage) (any, error) {
	var p protocol.DaemonSlugParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if err := validateDaemon(p.SiteID, p.Slug); err != nil {
		return nil, err
	}
	if p.Lines <= 0 {
		p.Lines = 200
	}
	if p.Lines > 5000 {
		p.Lines = 5000
	}
	out, err := runVettedHelper(ctx, "daemon_logs", []string{
		strconv.FormatInt(p.SiteID, 10),
		p.Slug,
		strconv.Itoa(p.Lines),
	})
	if err != nil {
		return nil, err
	}
	ok := out.(protocol.HostOKResult)
	return protocol.DaemonLogsResult{Output: ok.Stdout}, nil
}

func validateDaemon(siteID int64, slug string) error {
	if siteID < 1 || siteID > 1_000_000 {
		return errors.New("site_id out of range")
	}
	if slug == "" {
		return errors.New("slug required")
	}
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return errors.New("slug must be [a-z0-9-]+")
		}
	}
	return nil
}
