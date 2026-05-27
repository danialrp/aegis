// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// spaHandler serves the embedded SPA from spaFS.
//
// The contract:
//   - Requests for /assets/... and similar concrete files are served
//     straight from the FS with their real Content-Type.
//   - Any other GET that doesn't match a file falls back to index.html
//     so client-side routing (TanStack Router) can take over.
//   - Non-GET requests return 405 — this handler is for the frontend
//     only; API requests are routed before we get here.
//
// API paths (/v1/*, /healthz, /readyz) must be registered BEFORE
// this handler in the router; chi's tree handles the precedence.
// notBuiltPage is served when the embedded SPA contains no index.html —
// i.e. the controller was built without running `make web` first.
const notBuiltPage = `<!doctype html>
<html lang="en"><head>
<meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Aegis — frontend not built</title>
<style>body{font:14px/1.5 system-ui,sans-serif;margin:4rem auto;max-width:32rem;padding:0 1rem;color:#1f2937}
code{background:#f3f4f6;padding:.1rem .3rem;border-radius:.25rem;font-size:.9em}
h1{font-size:1.25rem;margin-bottom:.25rem}p{color:#4b5563}</style>
</head><body>
<h1>Aegis frontend not built</h1>
<p>The controller is running, but no SPA bundle was embedded at build time.
Run <code>make web</code> in the repo root to compile the React bundle,
then rebuild and restart the controller.</p>
<p>API endpoints under <code>/v1</code>, <code>/healthz</code>, and
<code>/readyz</code> are unaffected.</p>
</body></html>`

func spaHandler(spaFS fs.FS) http.HandlerFunc {
	fileServer := http.FileServer(http.FS(spaFS))

	// Check whether a real index.html was bundled. If not, every SPA
	// request renders notBuiltPage instead — clearer than a 404.
	indexAvailable := false
	if f, err := spaFS.Open("index.html"); err == nil {
		_ = f.Close()
		indexAvailable = true
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if !indexAvailable {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(notBuiltPage))
			return
		}

		urlPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if urlPath == "" {
			urlPath = "index.html"
		}

		f, err := spaFS.Open(urlPath)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			// Unknown path → SPA fallback. Serve index.html, let the
			// router resolve the route client-side (or render 404 there).
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		_ = f.Close()

		fileServer.ServeHTTP(w, r)
	}
}
