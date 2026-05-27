// SPDX-License-Identifier: AGPL-3.0-or-later

package provisioner

import (
	"fmt"
	"strings"
)

// SystemdUnit returns the contents of /etc/systemd/system/aegis-agent.service.
//
// The unit:
//   - runs as the unprivileged `aegis` user
//   - reads its env from /etc/aegis/aegis.env
//   - restarts on failure with a 5s back-off
//   - boots after the network is up
//
// The string is constant; no parameters — the env file carries per-
// install settings.
func SystemdUnit() string {
	return `[Unit]
Description=Aegis Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=aegis
Group=aegis
WorkingDirectory=/var/lib/aegis
EnvironmentFile=/etc/aegis/aegis.env
ExecStart=/usr/local/bin/aegis-agent
Restart=on-failure
RestartSec=5s

# Sane hardening defaults. The agent is not yet a daemon-running-as-root
# host primitive owner — when those land, ProtectSystem may need easing.
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/aegis

[Install]
WantedBy=multi-user.target
`
}

// EnvFile returns the contents of /etc/aegis/aegis.env. It carries
// the four AEGIS_* variables the agent needs at startup.
//
// Values are quoted with a strict shell escape that only accepts
// printable ASCII without the characters that would break double-
// quoted strings. The caller is responsible for not passing values
// outside that set.
func EnvFile(controllerURL string) string {
	var b strings.Builder
	b.WriteString("# Aegis agent environment — provisioned by aegis-controller.\n")
	b.WriteString("# Do not edit by hand; this file is overwritten on reprovision.\n\n")
	writeEnv(&b, "AEGIS_CONTROLLER_URL", controllerURL)
	writeEnv(&b, "AEGIS_CONTROLLER_CA", "/etc/aegis/controller-ca.crt")
	writeEnv(&b, "AEGIS_AGENT_CERT", "/etc/aegis/agent.crt")
	writeEnv(&b, "AEGIS_AGENT_KEY", "/etc/aegis/agent.key")
	writeEnv(&b, "AEGIS_LOG_FORMAT", "json")
	writeEnv(&b, "AEGIS_LOG_LEVEL", "info")
	return b.String()
}

func writeEnv(b *strings.Builder, key, value string) {
	fmt.Fprintf(b, "%s=%q\n", key, value)
}

// ArchFromUname maps `uname -m` output to a Go GOARCH value. Returns
// an error for architectures we have not built an agent binary for.
func ArchFromUname(uname string) (string, error) {
	switch strings.TrimSpace(uname) {
	case "x86_64", "amd64":
		return "amd64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported target architecture: %q", strings.TrimSpace(uname))
	}
}
