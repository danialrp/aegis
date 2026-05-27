// SPDX-License-Identifier: AGPL-3.0-or-later

// Package version exposes build-time version metadata.
//
// Values are populated via -ldflags at build time. See the Makefile's
// LDFLAGS variable for the canonical invocation.
package version

import "fmt"

// These variables are overridden at link time via -X flags.
// Defaults keep `go run`/`go build` (without ldflags) honest about origin.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns a human-readable build identifier.
func String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date)
}
