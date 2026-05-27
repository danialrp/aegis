// SPDX-License-Identifier: AGPL-3.0-or-later

package v1

import (
	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/danialrp/aegis/internal/agentbus"
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
	Auth             *auth.Service
	JWTSecret        []byte
	Queries          *sqlc.Queries
	Audit            *audit.Recorder
	RiverClient      *river.Client[pgx.Tx]
	Hub              *agentbus.Hub
	LetsEncryptEmail string
	Logger           *slog.Logger
}

// Mount registers the /v1 route tree under r.
func Mount(r chi.Router, d MountDeps) {
	authH := NewAuthHandler(d.Auth, d.Logger)
	serversH := NewServersHandler(d.Queries, d.Audit, d.RiverClient, d.Logger)
	sitesH := NewSitesHandler(d.Queries, d.Audit, d.RiverClient, d.Logger)
	deploysH := NewDeploysHandler(d.Queries, d.Audit, d.RiverClient, d.Logger)
	sslH := NewSSLHandler(d.Queries, d.Audit, d.RiverClient, d.LetsEncryptEmail, d.Logger)
	daemonsH := NewDaemonsHandler(d.Queries, d.Audit, d.Hub, d.Logger)
	dockerH := NewDockerHandler(d.Queries, d.Audit, d.Hub, d.Logger)

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

		// Public webhook entry — secret is the auth, not a session.
		r.Post("/webhooks/git/{site_id}", deploysH.Webhook)

		// Authenticated, role-gated everything else.
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(d.Auth, d.JWTSecret))
			r.Use(middleware.RequireRole(auth.RoleGod, auth.RoleAdmin))

			r.Get("/servers", serversH.List)
			r.Get("/servers/{id}", serversH.Get)
			r.Post("/servers", serversH.Create)
			r.Delete("/servers/{id}", serversH.Delete)

			r.Get("/sites", sitesH.List)
			r.Get("/sites/{id}", sitesH.Get)
			r.Post("/sites", sitesH.Create)
			r.Delete("/sites/{id}", sitesH.Delete)

			r.Get("/sites/{id}/deploy-script", deploysH.GetScript)
			r.Put("/sites/{id}/deploy-script", deploysH.PutScript)

			r.Get("/sites/{id}/deploys", deploysH.ListDeploys)
			r.Post("/sites/{id}/deploys", deploysH.CreateDeploy)
			r.Get("/deploys/{deploy_id}", deploysH.GetDeploy)

			// SSL — Phase 2.1 / 2.2.
			r.Get("/sites/{id}/certs", sslH.ListCerts)
			r.Post("/sites/{id}/certs", sslH.CreateCert)
			r.Delete("/sites/{id}/certs/{cert_id}", sslH.DeleteCert)

			// Daemons — Phase 2.4 / 2.5.
			r.Get("/sites/{id}/daemons", daemonsH.List)
			r.Post("/sites/{id}/daemons", daemonsH.Create)
			r.Delete("/sites/{id}/daemons/{daemon_id}", daemonsH.Delete)
			r.Post("/sites/{id}/daemons/{daemon_id}/action", daemonsH.Action)
			r.Get("/sites/{id}/daemons/{daemon_id}/logs", daemonsH.Logs)

			// Docker compose — Phase 3.
			r.Get("/sites/{id}/compose", dockerH.GetCompose)
			r.Put("/sites/{id}/compose", dockerH.PutCompose)
			r.Post("/sites/{id}/compose/action", dockerH.Action)
			r.Get("/sites/{id}/containers", dockerH.ListContainers)
			r.Get("/sites/{id}/containers/{service}/logs", dockerH.ContainerLogs)
		})
	})
}
