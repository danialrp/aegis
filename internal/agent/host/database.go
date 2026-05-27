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

// HandleDBCreate handles host.mysql_db_create / host.postgres_db_create
// (the agent dispatcher routes both methods here via the same handler;
// the engine is on the params).
func (m *Manager) HandleDBCreate(ctx context.Context, params json.RawMessage) (any, error) {
	var p protocol.DBCreateParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if err := validateDBCreate(&p); err != nil {
		return nil, err
	}
	switch p.Engine {
	case "mysql":
		return runVettedHelper(ctx, "mysql_db_create", []string{
			strconv.FormatInt(p.SiteID, 10),
			p.Name, p.Username, p.Password,
		})
	case "postgres":
		return runVettedHelper(ctx, "postgres_db_create", []string{
			p.Name, p.Username, p.Password,
		})
	default:
		return nil, errors.New("invalid engine")
	}
}

// HandleDBDrop handles host.mysql_db_drop / host.postgres_db_drop.
func (m *Manager) HandleDBDrop(ctx context.Context, params json.RawMessage) (any, error) {
	var p protocol.DBDropParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if !validDBName(p.Name) || !validDBUser(p.Username) {
		return nil, errors.New("invalid name or user")
	}
	switch p.Engine {
	case "mysql":
		return runVettedHelper(ctx, "mysql_db_drop", []string{p.Name, p.Username})
	case "postgres":
		return runVettedHelper(ctx, "postgres_db_drop", []string{p.Name, p.Username})
	default:
		return nil, errors.New("invalid engine")
	}
}

// HandleDBBackup handles host.db_backup.
func (m *Manager) HandleDBBackup(ctx context.Context, params json.RawMessage) (any, error) {
	var p protocol.DBBackupParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if p.SiteID < 1 || p.SiteID > 1_000_000 {
		return nil, errors.New("site_id out of range")
	}
	if p.Engine != "mysql" && p.Engine != "postgres" {
		return nil, errors.New("invalid engine")
	}
	if !validDBName(p.Name) {
		return nil, errors.New("invalid db_name")
	}
	out, err := runVettedHelper(ctx, "db_backup", []string{
		strconv.FormatInt(p.SiteID, 10),
		p.Engine, p.Name,
	})
	if err != nil {
		return nil, err
	}
	ok := out.(protocol.HostOKResult)
	// The helper's stdout is the filename it wrote.
	return protocol.DBBackupResult{Path: trimNewline(ok.Stdout)}, nil
}

// HandleDBRestore handles host.db_restore.
func (m *Manager) HandleDBRestore(ctx context.Context, params json.RawMessage) (any, error) {
	var p protocol.DBRestoreParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if p.SiteID < 1 || p.SiteID > 1_000_000 {
		return nil, errors.New("site_id out of range")
	}
	if p.Engine != "mysql" && p.Engine != "postgres" {
		return nil, errors.New("invalid engine")
	}
	if !validDBName(p.Name) {
		return nil, errors.New("invalid db_name")
	}
	if !validBackupBasename(p.Basename) {
		return nil, errors.New("invalid backup basename")
	}
	return runVettedHelper(ctx, "db_restore", []string{
		strconv.FormatInt(p.SiteID, 10),
		p.Engine, p.Name, p.Basename,
	})
}

// HandleDBBackupsList handles host.db_backups_list.
func (m *Manager) HandleDBBackupsList(ctx context.Context, params json.RawMessage) (any, error) {
	var p protocol.SiteIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if p.SiteID < 1 || p.SiteID > 1_000_000 {
		return nil, errors.New("site_id out of range")
	}
	out, err := runVettedHelper(ctx, "db_backups_list", []string{strconv.FormatInt(p.SiteID, 10)})
	if err != nil {
		return nil, err
	}
	ok := out.(protocol.HostOKResult)
	return protocol.DBBackupsListResult{Raw: ok.Stdout}, nil
}

// --- validators ---

func validateDBCreate(p *protocol.DBCreateParams) error {
	if p.Engine != "mysql" && p.Engine != "postgres" {
		return errors.New("invalid engine")
	}
	if !validDBName(p.Name) {
		return errors.New("invalid db_name")
	}
	if !validDBUser(p.Username) {
		return errors.New("invalid db_user")
	}
	if p.Password == "" {
		return errors.New("password required")
	}
	if p.Engine == "mysql" && (p.SiteID < 1 || p.SiteID > 1_000_000) {
		return errors.New("site_id out of range")
	}
	return nil
}

func validDBName(s string) bool {
	if len(s) == 0 || len(s) > 63 {
		return false
	}
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

func validDBUser(s string) bool {
	if len(s) == 0 || len(s) > 32 {
		return false
	}
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

func validBackupBasename(s string) bool {
	if s == "" || len(s) > 200 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
