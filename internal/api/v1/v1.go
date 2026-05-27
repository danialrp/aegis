// SPDX-License-Identifier: AGPL-3.0-or-later

package v1

import (
	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/danialrp/aegis/internal/api/middleware"
	"github.com/danialrp/aegis/internal/audit"
	"github.com/danialrp/aegis/internal/auth"
	"github.com/danialrp/aegis/internal/db/sqlc"
)

// Login rate-limit knobs: refill ~one token every 3 minutes, burst 5.
// This lets an honest user fat-finger five times before they're slowed.
const (
	loginRatePerSecond = 1.0 / 180.0
	loginRateBurst     = 5
)

// MountDeps carries every dependency v1.Mount needs to wire its
// route tree. Adding a new domain handler means adding a field here
// and a route below, not threading another arg through Mount.
type MountDeps struct {
	Auth        *auth.Service
	JWTSecret   []byte
	Queries     *sqlc.Queries
	Audit       *audit.Recorder
	RiverClient *river.Client[pgx.Tx]
	Logger      *slog.Logger
}

// Mount registers the /v1 route tree under r.
func Mount(r chi.Router, d MountDeps) {
	authH := NewAuthHandler(d.Auth, d.Logger)
	serversH := NewServersHandler(d.Queries, d.Audit, d.RiverClient, d.Logger)

	r.Route("/v1", func(r chi.Router) {
		r.Route("/auth", func(r chi.Router) {
			r.Group(func(r chi.Router) {
				r.Use(middleware.NewLoginRateLimiter(loginRatePerSecond, loginRateBurst))
				r.Post("/login", authH.Login)
				r.Post("/refresh", authH.Refresh)
			})

			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireAuth(d.Auth, d.JWTSecret))
				r.Get("/me", authH.Me)
				r.Post("/logout", authH.Logout)
			})
		})

		// All servers endpoints require auth + an elevated role.
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(d.Auth, d.JWTSecret))
			r.Use(middleware.RequireRole(auth.RoleGod, auth.RoleAdmin))
			r.Get("/servers", serversH.List)
			r.Get("/servers/{id}", serversH.Get)
			r.Post("/servers", serversH.Create)
			r.Delete("/servers/{id}", serversH.Delete)
		})
	})
}
