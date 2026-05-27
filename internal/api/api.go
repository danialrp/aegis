// SPDX-License-Identifier: AGPL-3.0-or-later

// Package api wires the controller's HTTP surface: chi router,
// middleware stack, and route registration.
package api

import (
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/danialrp/aegis/internal/agentbus"
	"github.com/danialrp/aegis/internal/api/middleware"
	v1 "github.com/danialrp/aegis/internal/api/v1"
	"github.com/danialrp/aegis/internal/audit"
	"github.com/danialrp/aegis/internal/auth"
	"github.com/danialrp/aegis/internal/db/sqlc"
)

// Deps are the runtime dependencies the router needs to wire handlers.
// Add fields here as new domains come online; do not pull globals into
// handlers.
type Deps struct {
	Logger           *slog.Logger
	Pool             *pgxpool.Pool
	AuthService      *auth.Service
	JWTSecret        []byte
	Queries          *sqlc.Queries
	Audit            *audit.Recorder
	RiverClient      *river.Client[pgx.Tx]
	Hub              *agentbus.Hub
	LetsEncryptEmail string
	// SPA is the embedded React build (web.FS). Optional — when nil,
	// the controller serves a tiny placeholder page at /. Tests can
	// safely omit it.
	SPA fs.FS
}

// NewRouter returns the controller's top-level HTTP handler with the
// standard middleware stack, health endpoints, /v1 API, and (when
// provided) the embedded SPA.
func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	// chimw.RealIP is intentionally omitted — it's deprecated upstream
	// (GHSA-3fxj-6jh8-hvhx) because it trusts client-supplied headers.
	r.Use(chimw.Recoverer)
	r.Use(middleware.SecurityHeaders)
	r.Use(requestLogger(deps.Logger))

	r.Get("/healthz", healthzHandler)
	r.Get("/readyz", readyzHandler(deps.Pool))

	if deps.AuthService != nil {
		v1.Mount(r, v1.MountDeps{
			Auth:             deps.AuthService,
			JWTSecret:        deps.JWTSecret,
			Queries:          deps.Queries,
			Audit:            deps.Audit,
			RiverClient:      deps.RiverClient,
			Hub:              deps.Hub,
			LetsEncryptEmail: deps.LetsEncryptEmail,
			Logger:           deps.Logger,
		})
	}

	// SPA must register LAST so /healthz, /readyz, /v1/* take
	// precedence. NotFound catches everything else — typical
	// SPA-fallback pattern.
	if deps.SPA != nil {
		r.NotFound(spaHandler(deps.SPA))
	}

	return r
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			logger.LogAttrs(r.Context(), slog.LevelInfo, "http",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", ww.Status()),
				slog.Int("bytes", ww.BytesWritten()),
				slog.Duration("dur", time.Since(start)),
				slog.String("request_id", chimw.GetReqID(r.Context())),
			)
		})
	}
}
