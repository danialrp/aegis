// SPDX-License-Identifier: AGPL-3.0-or-later

// Package web embeds the built React SPA so the controller binary can
// serve the entire frontend itself — no separate static-file server,
// no CDN, no Caddy fronting.
//
// The Vite output lives at web/dist/, populated by `make web`. The
// directory is committed with a stub `index.html` only so that
// `go build` works on a fresh checkout before the JS toolchain has run.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// FS returns the built SPA rooted at dist/.
//
// The returned filesystem treats `/` as the project root, so callers
// should read `index.html`, `assets/...`, etc. directly. Returns an
// error only if the embedded subtree is malformed — which would
// indicate a build-system bug, not a runtime condition.
func FS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
