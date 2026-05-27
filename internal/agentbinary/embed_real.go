// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build embedagent

// Package agentbinary exposes the cross-compiled aegis-agent binaries
// that the controller pushes to managed servers during bootstrap.
//
// Build with `-tags=embedagent` after `make agent-cross` has produced
// the two ELF files. The non-tagged build (the default) provides an
// empty implementation so `go build` works on a fresh checkout.
package agentbinary

import (
	"embed"
	"fmt"
)

//go:embed bin/linux-amd64/aegis-agent bin/linux-arm64/aegis-agent
var fs embed.FS

// minAcceptableSize guards against accidentally embedding a 0-byte
// placeholder: a real Go binary is well over a megabyte.
const minAcceptableSize = 1 * 1024 * 1024

// For returns the agent binary for the given GOARCH ("amd64" or
// "arm64"). The byte slice is a fresh copy — safe for the caller to
// mutate.
func For(arch string) ([]byte, error) {
	path := fmt.Sprintf("bin/linux-%s/aegis-agent", arch)
	data, err := fs.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read embedded agent binary %q: %w", path, err)
	}
	if len(data) < minAcceptableSize {
		return nil, fmt.Errorf(
			"embedded agent binary for %s is %d bytes — placeholder? run `make agent-cross`",
			arch, len(data))
	}
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}

// Embedded reports that this build actually carries the binaries.
func Embedded() bool { return true }
