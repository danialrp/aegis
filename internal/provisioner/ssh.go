// SPDX-License-Identifier: AGPL-3.0-or-later

package provisioner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	defaultSSHTimeout = 30 * time.Second
	uploadChunkLimit  = 64 * 1024 * 1024 // sanity cap on upload size
)

// SSHProvisioner is the production implementation of Provisioner.
type SSHProvisioner struct {
	dialTimeout time.Duration
}

// NewSSH builds an SSHProvisioner with sensible default timeouts.
func NewSSH() *SSHProvisioner {
	return &SSHProvisioner{dialTimeout: defaultSSHTimeout}
}

// Provision walks the bootstrap steps against t.Host:t.Port.
//
// On any step failure the returned error wraps ErrBootstrapFailed so
// the caller can distinguish remote-side failures from local bugs.
func (p *SSHProvisioner) Provision(ctx context.Context, t Target) error {
	log := t.Logger
	if log == nil {
		log = slog.Default()
	}
	log = log.With("server_id", t.ServerID, "host", t.Host)

	client, err := p.dial(ctx, t)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBootstrapFailed, err)
	}
	defer func() { _ = client.Close() }()

	log.Info("ssh connected, detecting arch")
	uname, err := runOutput(client, "uname -m")
	if err != nil {
		return fmt.Errorf("%w: detect arch: %w", ErrBootstrapFailed, err)
	}
	arch, err := ArchFromUname(uname)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBootstrapFailed, err)
	}
	log = log.With("arch", arch)

	binary, err := t.AgentBinaryFor(arch)
	if err != nil {
		return fmt.Errorf("%w: agent binary: %w", ErrBootstrapFailed, err)
	}

	for _, step := range []struct {
		name string
		fn   func() error
	}{
		{"ensure user", func() error { return runSudoSteps(client, ensureUserCmds()) }},
		{"ensure dirs", func() error { return runSudoSteps(client, ensureDirsCmds()) }},
		{"upload binary", func() error {
			return uploadFile(client, binary, "/usr/local/bin/aegis-agent", "0755", "root:root")
		}},
		{"upload ca", func() error {
			return uploadFile(client, t.ControllerCA, "/etc/aegis/controller-ca.crt", "0644", "aegis:aegis")
		}},
		{"upload cert", func() error {
			return uploadFile(client, t.AgentCertPEM, "/etc/aegis/agent.crt", "0644", "aegis:aegis")
		}},
		{"upload key", func() error {
			return uploadFile(client, t.AgentKeyPEM, "/etc/aegis/agent.key", "0600", "aegis:aegis")
		}},
		{"write env", func() error {
			return uploadFile(client, []byte(EnvFile(t.ControllerURL)), "/etc/aegis/aegis.env", "0640", "aegis:aegis")
		}},
		{"write unit", func() error {
			return uploadFile(client, []byte(SystemdUnit()), "/etc/systemd/system/aegis-agent.service", "0644", "root:root")
		}},
		{"start service", func() error {
			return runSudoSteps(client, []string{
				"systemctl daemon-reload",
				"systemctl enable aegis-agent",
				"systemctl restart aegis-agent",
			})
		}},
	} {
		if err := ctx.Err(); err != nil {
			return err
		}
		log.Info("step", "name", step.name)
		if err := step.fn(); err != nil {
			return fmt.Errorf("%w: %s: %w", ErrBootstrapFailed, step.name, err)
		}
	}

	log.Info("agent installed; service started")
	return nil
}

// --- internals ---

func (p *SSHProvisioner) dial(ctx context.Context, t Target) (*ssh.Client, error) {
	authMethods, err := buildAuth(t)
	if err != nil {
		return nil, err
	}

	cfg := &ssh.ClientConfig{
		User:    t.User,
		Auth:    authMethods,
		Timeout: p.dialTimeout,
		// 0.8: trust-on-first-use. A persistent host-key store lands
		// when we re-provision / change-IP flows arrive.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // documented TOFU
	}

	addr := net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
	d := &net.Dialer{Timeout: p.dialTimeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("tcp dial %s: %w", addr, err)
	}

	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ssh handshake: %w", err)
	}
	return ssh.NewClient(c, chans, reqs), nil
}

func buildAuth(t Target) ([]ssh.AuthMethod, error) {
	switch {
	case len(t.PrivateKey) > 0:
		signer, err := ssh.ParsePrivateKey(t.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	case t.Password != "":
		return []ssh.AuthMethod{ssh.Password(t.Password)}, nil
	default:
		return nil, errors.New("no SSH auth method provided")
	}
}

func runOutput(client *ssh.Client, cmd string) (string, error) {
	sess, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("new session: %w", err)
	}
	defer func() { _ = sess.Close() }()

	var out bytes.Buffer
	sess.Stdout = &out
	if err := sess.Run(cmd); err != nil {
		return "", fmt.Errorf("run %q: %w", cmd, err)
	}
	return out.String(), nil
}

// runSudoSteps runs each command via `sudo -n sh -c` so it surfaces
// promptly if the SSH user doesn't have passwordless sudo. Output is
// captured and surfaced in errors.
func runSudoSteps(client *ssh.Client, cmds []string) error {
	for _, c := range cmds {
		if err := runCheck(client, "sudo -n sh -c "+shellQuote(c)); err != nil {
			return err
		}
	}
	return nil
}

func runCheck(client *ssh.Client, cmd string) error {
	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer func() { _ = sess.Close() }()

	var stderr bytes.Buffer
	sess.Stderr = &stderr
	if err := sess.Run(cmd); err != nil {
		return fmt.Errorf("run %q: %w (stderr: %s)", cmd, err, stderr.String())
	}
	return nil
}

// uploadFile streams content to dest via `sudo tee`, then chmods +
// chowns. Returns an error if size exceeds uploadChunkLimit.
func uploadFile(client *ssh.Client, content []byte, dest, mode, owner string) error {
	if len(content) > uploadChunkLimit {
		return fmt.Errorf("refusing to upload %d bytes (>%d)", len(content), uploadChunkLimit)
	}

	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer func() { _ = sess.Close() }()

	stdin, err := sess.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	cmd := fmt.Sprintf("sudo -n tee %s > /dev/null && sudo -n chmod %s %s && sudo -n chown %s %s",
		shellQuote(dest), mode, shellQuote(dest), owner, shellQuote(dest))
	var stderr bytes.Buffer
	sess.Stderr = &stderr

	if err := sess.Start(cmd); err != nil {
		return fmt.Errorf("start upload: %w", err)
	}

	if _, err := io.Copy(stdin, bytes.NewReader(content)); err != nil {
		_ = stdin.Close()
		_ = sess.Wait()
		return fmt.Errorf("copy: %w", err)
	}
	if err := stdin.Close(); err != nil {
		_ = sess.Wait()
		return fmt.Errorf("close stdin: %w", err)
	}
	if err := sess.Wait(); err != nil {
		return fmt.Errorf("upload to %s: %w (stderr: %s)", dest, err, stderr.String())
	}
	return nil
}

func ensureUserCmds() []string {
	// `id -u aegis` returns nonzero if the user is absent; in that case
	// we create it. The compound test/use shell pattern keeps it idempotent.
	return []string{
		`id -u aegis >/dev/null 2>&1 || useradd --system --shell /usr/sbin/nologin --home /var/lib/aegis --create-home aegis`,
	}
}

func ensureDirsCmds() []string {
	return []string{
		"install -d -o aegis -g aegis -m 0750 /etc/aegis",
		"install -d -o aegis -g aegis -m 0750 /var/lib/aegis",
	}
}

// shellQuote wraps s in single quotes, escaping any inline single
// quotes. All our inputs are constants today, but defending the
// quoting boundary anyway costs nothing.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
