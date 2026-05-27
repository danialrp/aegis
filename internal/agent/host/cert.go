// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/danialrp/aegis/pkg/protocol"
)

// HandleCertIssue handles host.cert_issue.
//
// Runs `certbot --nginx` for the given domain. nginx must already be
// serving the domain on port 80 (HTTP-01). On success certbot will
// also rewrite the existing vhost to add the 443 server block + 301
// redirect from port 80.
func (m *Manager) HandleCertIssue(ctx context.Context, params json.RawMessage) (any, error) {
	var p protocol.CertIssueParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if !validDomain(p.Domain) {
		return nil, errors.New("invalid domain")
	}
	if !validEmail(p.Email) {
		return nil, errors.New("invalid email")
	}
	// certbot issuance can be slow — use the caller's deadline rather
	// than the default helperTimeout.
	return runVettedHelper(ctx, "cert_issue", []string{p.Domain, p.Email})
}

// HandleCertRemove handles host.cert_remove.
func (m *Manager) HandleCertRemove(ctx context.Context, params json.RawMessage) (any, error) {
	var p protocol.DomainParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if !validDomain(p.Domain) {
		return nil, errors.New("invalid domain")
	}
	return runVettedHelper(ctx, "cert_remove", []string{p.Domain})
}

// HandleCertStatus handles host.cert_status. Returns the raw text
// from `certbot certificates`; the controller parses expiry per cert.
func (m *Manager) HandleCertStatus(ctx context.Context, _ json.RawMessage) (any, error) {
	out, err := runVettedHelper(ctx, "cert_status", nil)
	if err != nil {
		return nil, err
	}
	ok := out.(protocol.HostOKResult)
	return protocol.CertStatusResult{Raw: ok.Stdout}, nil
}

// runVettedHelper is the slightly-more-generic sibling of runHelper:
// takes pre-validated string args instead of a SiteIDParams. The
// helper itself revalidates every arg.
func runVettedHelper(ctx context.Context, helper string, args []string) (any, error) {
	helperPath := helperDir + "/" + helper

	allArgs := append([]string{"-n", "--", helperPath}, args...)
	//nolint:gosec // helperPath is constant; args were validated by the caller
	// and are revalidated by the helper itself.
	cmd := exec.CommandContext(ctx, "sudo", allArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %w (stderr: %s)", helper, err, stderr.String())
	}
	return protocol.HostOKResult{Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

// validDomain mirrors the helper's POSIX-sh check: lowercase letters,
// digits, dots, hyphens; no leading dot or double-dot or edge-hyphen.
func validDomain(d string) bool {
	if d == "" || d[0] == '.' {
		return false
	}
	for i, r := range d {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '-':
		default:
			return false
		}
		if r == '.' && i+1 < len(d) && d[i+1] == '.' {
			return false
		}
	}
	return !strings.Contains(d, "-.") && !strings.Contains(d, ".-")
}

// validEmail is intentionally a coarse charset filter — the helper
// + certbot reject malformed addresses authoritatively.
func validEmail(e string) bool {
	if e == "" || !strings.Contains(e, "@") {
		return false
	}
	for _, r := range e {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '+' || r == '_' || r == '-' || r == '@':
		default:
			return false
		}
	}
	return true
}
