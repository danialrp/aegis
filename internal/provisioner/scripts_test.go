// SPDX-License-Identifier: AGPL-3.0-or-later

package provisioner_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/danialrp/aegis/internal/provisioner"
)

func TestSystemdUnitShape(t *testing.T) {
	t.Parallel()
	u := provisioner.SystemdUnit()
	require.Contains(t, u, "User=aegis")
	require.Contains(t, u, "ExecStart=/usr/local/bin/aegis-agent")
	require.Contains(t, u, "EnvironmentFile=/etc/aegis/aegis.env")
	require.Contains(t, u, "Restart=on-failure")
	require.Contains(t, u, "NoNewPrivileges=true")
}

func TestEnvFileContainsAllVars(t *testing.T) {
	t.Parallel()
	got := provisioner.EnvFile("wss://controller.example.com:8443/agent/v1/ws")
	for _, expected := range []string{
		`AEGIS_CONTROLLER_URL="wss://controller.example.com:8443/agent/v1/ws"`,
		`AEGIS_CONTROLLER_CA="/etc/aegis/controller-ca.crt"`,
		`AEGIS_AGENT_CERT="/etc/aegis/agent.crt"`,
		`AEGIS_AGENT_KEY="/etc/aegis/agent.key"`,
	} {
		require.Truef(t, strings.Contains(got, expected),
			"env file missing %q\ngot:\n%s", expected, got)
	}
}

func TestArchFromUname(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"x86_64\n": "amd64",
		"amd64":    "amd64",
		"aarch64":  "arm64",
		"arm64\n":  "arm64",
	}
	for input, want := range cases {
		t.Run(strings.TrimSpace(input), func(t *testing.T) {
			got, err := provisioner.ArchFromUname(input)
			require.NoError(t, err)
			require.Equal(t, want, got)
		})
	}

	_, err := provisioner.ArchFromUname("riscv64")
	require.Error(t, err)
}
