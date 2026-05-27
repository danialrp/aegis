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

// HandlePhpFpmPoolWrite handles host.php_fpm_pool_write.
func (m *Manager) HandlePhpFpmPoolWrite(ctx context.Context, params json.RawMessage) (any, error) {
	var p protocol.SiteIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if p.SiteID < 1 || p.SiteID > 1_000_000 {
		return nil, errors.New("site_id out of range")
	}
	return runVettedHelper(ctx, "php_fpm_pool_write", []string{strconv.FormatInt(p.SiteID, 10)})
}

// HandlePhpFpmPoolRemove handles host.php_fpm_pool_remove.
func (m *Manager) HandlePhpFpmPoolRemove(ctx context.Context, params json.RawMessage) (any, error) {
	var p protocol.SiteIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if p.SiteID < 1 || p.SiteID > 1_000_000 {
		return nil, errors.New("site_id out of range")
	}
	return runVettedHelper(ctx, "php_fpm_pool_remove", []string{strconv.FormatInt(p.SiteID, 10)})
}

// HandleNginxApplyPhpVhost handles host.nginx_apply_php_vhost.
func (m *Manager) HandleNginxApplyPhpVhost(ctx context.Context, params json.RawMessage) (any, error) {
	var p protocol.NginxApplyPhpVhostParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if p.SiteID < 1 || p.SiteID > 1_000_000 {
		return nil, errors.New("site_id out of range")
	}
	if !validDomain(p.Domain) {
		return nil, errors.New("invalid domain")
	}
	if p.WorkingDir == "" {
		return nil, errors.New("working_dir required")
	}
	return runVettedHelper(ctx, "nginx_write_php_vhost", []string{
		strconv.FormatInt(p.SiteID, 10),
		p.Domain,
		p.WorkingDir,
	})
}
