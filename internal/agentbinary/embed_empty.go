// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !embedagent

// Package agentbinary exposes the cross-compiled aegis-agent binaries
// in builds tagged with `-tags=embedagent`. In the default build
// (this file), For always returns ErrNotEmbedded so a controller can
// still start during local dev — Add-Server will fail explicitly
// instead of silently uploading nothing.
package agentbinary

import "errors"

// ErrNotEmbedded is returned by For in builds that did not embed the
// agent binaries. Surface this verbatim from API handlers so
// operators see a useful next step.
var ErrNotEmbedded = errors.New(
	"agent binaries not embedded — run `make agent-cross` then rebuild with `-tags=embedagent`")

// For always returns ErrNotEmbedded in non-embedded builds.
func For(_ string) ([]byte, error) { return nil, ErrNotEmbedded }

// Embedded reports that this build does NOT carry the binaries.
func Embedded() bool { return false }
