# Aegis

**Self-hosted server panel with per-site isolation.**

Aegis is a modern, open-source server management panel for Linux servers.
Think of it as a Laravel Forge alternative that takes security seriously:
every site on a server gets its own Linux user, its own PHP-FPM pool, its
own Docker scope, and its own terminal — so a compromise (or a curious
developer) in one site cannot reach another site's files, secrets, or
containers.

> **Status:** Pre-release, in active development. Not yet ready for
> production use. See the [Roadmap](#roadmap) below.

---

## Why Aegis

Existing server panels (Laravel Forge, Ploi, RunCloud) use a single
"deploy user" model: one Linux user owns every site on the server. If
that user is compromised or a tenant gains shell access, every site on
the server is reachable. Newer Docker-first panels (Coolify, Dokploy,
CapRover) force their own reverse proxy (Traefik) onto the server,
fighting hand-tuned nginx setups.

Aegis takes a different stance:

- **Per-site Linux user**, owned home directory at mode `700`, dedicated
  PHP-FPM pool, dedicated supervisor scope, dedicated cron entries.
- **Per-site Docker scope** via a docker-socket-proxy per site, so a
  user with shell access cannot manage other sites' containers.
- **Keeps host nginx** as the front door. No bundled Traefik/Caddy. If
  you have battle-tested nginx configs, Aegis respects them and generates
  vhosts in the same style.
- **Outbound-only agent** — the controller never opens an inbound port
  on managed servers. Agents dial back over mTLS+WebSocket. Standard
  modern infrastructure pattern.
- **Real RBAC** — god user, site users, teams, per-site permission
  grants (Read / Execute / Write, with separate gates for Logs,
  Terminal, and Inspect).
- **Per-site deploy script** — every site has its own freely-editable
  deploy script (think Forge's Quick Deploy: `git pull`,
  `composer install`, `npm run build`, `php artisan migrate --force`,
  whatever). Scripts run as the site's Linux user — never root —
  with the site's environment variables loaded. Triggered by git
  push webhook, manual UI click, or schedule. Output streams live
  to the panel.

---

## Features

### Implemented
_Nothing yet — Phase 0 in progress._

### Roadmap

**Phase 0 — Foundations** _(in progress)_
- Controller + agent skeletons, mTLS bootstrap, Postgres + Redis,
  React + shadcn/ui dashboard scaffold, server provisioning.

**Phase 1 — Per-site Linux user isolation + deploy scripts**
- `site_<id>` Linux users, PHP-FPM pool per site, nginx vhost
  generation, static-site deploy end-to-end.
- **Per-site deploy script** (free-form bash) with a monaco-based
  editor, version history, git-push webhook trigger (GitHub +
  GitLab), manual run button, scheduled runs (cron expression),
  and live-streamed output via WebSocket. Scripts execute as the
  site's Linux user inside the site's working directory with
  site env vars loaded.

**Phase 2 — SSL + daemons**
- Let's Encrypt issue/renew via certbot with UI + systemd timer,
  supervisor-managed daemon UI per site.

**Phase 3 — Docker site type + per-site Docker scoping**
- `docker-compose` site type, per-site docker-socket-proxy,
  opt-in rootless dockerd per user.

**Phase 4 — App-type adapters**
- Laravel (composer/artisan/horizon), Next.js (Node + supervisor),
  WordPress (PHP + MySQL bootstrap + wp-cli), static HTML/SPA.

**Phase 5 — Database management UI**
- Postgres + MySQL CRUD, scoped per site, backups & restores.

**Phase 6 — Web terminal + monitoring**
- xterm.js terminal spawning as the site's user, Prometheus
  node-exporter + dashboards.

**Phase 7 — RBAC + teams**
- God user, site users, team invites, per-site permissions.

**Phase 8 — Polish + OSS release**
- README, contributing guide, screenshots, security policy,
  architecture docs.

---

## Quick start _(once Phase 0 is complete)_

> The full panel runs in Docker. You do not need Go, Node, PostgreSQL,
> or Redis installed on your machine — just Docker.

```bash
git clone https://github.com/danialrp/aegis.git
cd aegis
cp .env.example .env       # edit secrets if you want; defaults work
docker compose up -d
open http://localhost:8080
```

That brings up the controller, PostgreSQL, and Redis. The web UI is
served by the controller binary itself (single-binary architecture).
Managed servers are added from the panel — no manual install on
target servers.

---

## Architecture

Aegis is a **controller + per-server agent** system written in Go.

```
┌────────────────────────────────────────────────────────────────┐
│  Controller (single Go binary, runs in Docker)                 │
│   - HTTP API (chi) + WebSocket hub                             │
│   - Embedded React SPA (//go:embed)                            │
│   - Background workers (river — Postgres-backed)               │
│   - State: PostgreSQL · Cache/pubsub: Redis                    │
└──────────────────────┬─────────────────────────────────────────┘
                       │  outbound mTLS + WebSocket
                       ▼
┌────────────────────────────────────────────────────────────────┐
│  Agent (single Go binary on each managed server)               │
│   - Native binary (NOT containerized) so it can manage host    │
│     systemd, nginx, /etc, Linux users, Docker — safely         │
│   - Installed automatically by the controller via SSH bootstrap│
│   - Per-site docker-socket-proxy containers for scoping        │
└────────────────────────────────────────────────────────────────┘
```

See [ARCHITECTURE.md](./ARCHITECTURE.md) for the full design rationale,
component breakdown, security boundaries, and the case for native-agent
vs containerized-agent.

---

## Tech stack

| Layer            | Choice |
|------------------|--------|
| Backend language | Go |
| HTTP router      | chi |
| Database         | PostgreSQL (via `pgx` + `sqlc` for type-safe queries) |
| Migrations       | goose |
| Cache / pubsub   | Redis |
| Background jobs  | river (Postgres-backed) |
| SSH              | `golang.org/x/crypto/ssh` |
| Docker           | `github.com/docker/docker/client` (official) |
| WebSocket        | `coder/websocket` |
| PTY              | `creack/pty` |
| Auth             | argon2id passwords, JWT sessions, mTLS for agents |
| Logging          | `slog` (stdlib structured logging) |
| Metrics          | Prometheus client |
| Frontend         | React 19 + Vite + TypeScript |
| UI components    | shadcn/ui + Tailwind v4 |
| Routing          | TanStack Router |
| Server state     | TanStack Query |
| Tables / charts  | TanStack Table, recharts |
| Terminal         | xterm.js |
| Tests            | Go stdlib + testify + testcontainers-go + Playwright |

---

## Security

Aegis takes security seriously — the project's reason for existing is
per-site isolation that other panels skip. See [SECURITY.md](./SECURITY.md)
for the security model, threat model, and how to report vulnerabilities.

---

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](./CONTRIBUTING.md) for
the development setup, code style, testing requirements, and PR process.

---

## Acknowledgements

Aegis stands on the shoulders of giants. See [NOTICE](./NOTICE) for full
attribution. Particular thanks to:

- **[Vito](https://github.com/vitodeploy/vito)** — reference for server
  provisioning patterns and Let's Encrypt flow.
- **[satnaing/shadcn-admin](https://github.com/satnaing/shadcn-admin)** —
  initial frontend scaffold.
- **[Laravel Forge](https://forge.laravel.com/)** — the project that
  made this category of tooling popular and that Aegis is, in the
  friendliest possible sense, trying to improve on.

---

## License

Aegis is licensed under the [GNU Affero General Public License v3.0](./LICENSE)
(AGPL-3.0). This means:

- You can use, modify, and self-host Aegis freely.
- If you offer Aegis (or a modified version) as a hosted service to
  third parties, you must publish your source modifications under the
  same license.

If AGPL-3.0 does not work for your use case, please open an issue to
discuss commercial licensing options.

---

_"Per-site isolation isn't a feature. It's the only correct default."_
