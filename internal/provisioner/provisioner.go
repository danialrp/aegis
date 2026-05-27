// SPDX-License-Identifier: AGPL-3.0-or-later

// Package provisioner installs the Aegis agent onto a fresh Linux
// host over SSH.
//
// The flow at a glance:
//
//  1. Open SSH connection (password or private-key auth)
//  2. Detect target arch via `uname -m`
//  3. Create the unprivileged `aegis` system user if absent
//  4. Lay down /etc/aegis and /var/lib/aegis with correct ownership
//  5. Upload the agent binary to /usr/local/bin/aegis-agent (0755)
//  6. Upload cert + key + CA bundle to /etc/aegis/ (0640, owned by aegis)
//  7. Write the systemd unit and env file
//  8. systemctl daemon-reload && enable && start aegis-agent
//
// All paths are constants — no user-controlled values reach the
// remote shell. Credentials passed in Target never persist past the
// call; the package does not log them.
package provisioner

import (
	"context"
	"errors"
	"log/slog"
)

// ErrBootstrapFailed is returned wrapping the underlying step error
// so callers can branch on "remote operation failed" generically.
var ErrBootstrapFailed = errors.New("agent bootstrap failed")

// Target is everything Provision needs to bring up an agent on one
// remote host. AgentBinaryFor lets the worker stay lazy about
// architecture: the provisioner calls it once it knows the answer
// from `uname -m`.
type Target struct {
	// SSH connection
	Host       string
	Port       int
	User       string
	Password   string // optional
	PrivateKey []byte // optional, PEM-encoded
	HostKeys   HostKeyVerifier

	// Server identity
	ServerID int64

	// Agent artifacts
	AgentBinaryFor func(arch string) ([]byte, error)
	AgentCertPEM   []byte
	AgentKeyPEM    []byte
	ControllerCA   []byte
	ControllerURL  string

	// Observability
	Logger *slog.Logger
}

// HostKeyVerifier decides whether to trust the remote host's SSH
// public key. The default Provisioner accepts any host on first
// contact (TOFU) and stores the fingerprint — production should
// supply a stricter verifier.
type HostKeyVerifier interface {
	Verify(host string, key []byte) error
}

// Provisioner is the operation our job invokes. The interface exists
// solely so tests can swap in a fake without touching real SSH.
type Provisioner interface {
	Provision(ctx context.Context, t Target) error
}
