# Aegis — Architecture

This document describes the design of Aegis, the rationale behind the
choices, and the security boundaries that the implementation must
preserve.

> **Reading order:** if you only have five minutes, read sections 1, 2,
> and 4. Section 3 is the long form of section 1.

---

## 1. Topology at a glance

Aegis has exactly two binaries and two stateful services:

```
                         ┌─────────────────────────────────┐
                         │  Operator's browser             │
                         │  React SPA over HTTPS           │
                         └────────────┬────────────────────┘
                                      │ HTTPS + WSS
                                      ▼
┌─────────────────────────────────────────────────────────────┐
│  Aegis Controller (Docker container)                        │
│  • Single Go binary, embedded React SPA via //go:embed      │
│  • chi HTTP router + WebSocket hub                          │
│  • river background workers                                 │
│  • Issues per-agent mTLS certs from an internal CA          │
│  • Stores state in PostgreSQL; cache/pubsub in Redis        │
└──────────┬──────────────────────────────────────────────────┘
           │  outbound mTLS + WSS  (agent dials controller)
           ▼
┌─────────────────────────────────────────────────────────────┐
│  Aegis Agent (one per managed server)                       │
│  • Single Go binary, runs as `aegis` system user via systemd│
│  • Sudoers rule: ONLY whitelisted helper scripts            │
│  • Manages: nginx vhosts, PHP-FPM pools, Linux site users,  │
│    supervisor units, certbot renewals, docker-socket-proxy  │
│    containers (one per site), local UFW rules               │
│  • Streams logs / metrics / PTY back to the controller      │
└─────────────────────────────────────────────────────────────┘
```

Network direction is important: **agents always dial out to the
controller, never the other way around.** This means managed servers
expose no Aegis-related inbound port. The only inbound port on a
managed server is whatever the user's sites need (80/443).

---

## 2. The four security boundaries

Aegis enforces isolation at four layers. None of them is novel; the
contribution is composing them coherently in one panel.

| Boundary | Mechanism | What it stops |
|---|---|---|
| **Per-site filesystem** | OS file permissions: `site_<id>` owns `/srv/sites/<id>` mode `700` | Site A reading Site B's `.env`, code, uploads |
| **Per-site process** | PHP-FPM pool runs as `site_<id>`; supervisor units run as `site_<id>` | Site A's PHP code reading Site B's process memory or files |
| **Per-site Docker** | Each site gets a docker-socket-proxy that filters `docker` API calls to containers labeled `aegis.site_id=<id>` | Site A's user managing/inspecting Site B's containers via Docker CLI |
| **Per-site terminal** | Web terminal spawns `su - site_<id>` inside a PTY; the agent enforces this — there is no path to a root shell from the UI | Site A's user reading Site B's home dir even via the embedded shell |
| **Per-site deploy script** | Free-form bash, but the agent always invokes it via `sudo -u site_<id> -H -- bash -c <script>` with explicit `cwd=/srv/sites/<id>` and only the site's environment loaded. Never runs as root. Cannot `cd ..` out of the site's home (enforced by chroot-like working-directory restriction in the agent's exec wrapper). | Site A's deploy script doing `cp -r /srv/sites/<other>/.env .` (it fails — site_A has no read permission on Site B's directory regardless of what the script tries) |

The **god user** is a separate, panel-level superuser. It can see every
site and operate on any server. There is no path from a site-scoped UI
session to god privileges — privilege escalation is an explicit
sign-in as the god account, not a setting.

Aegis itself has **no privilege escalation API endpoints.** A site user
cannot "request" elevated access from the UI. If a site user needs more,
the god user grants it as a deliberate, audit-logged change.

---

## 3. Why these choices

### Why Go (not Rust)

Aegis is I/O-bound: SSH round-trips, Docker API calls, Postgres
queries, WebSocket fan-out. None of it is CPU-bound. Go's mature
ecosystem (`golang.org/x/crypto/ssh`, official Docker client, `pgx`,
`go-redis`) is decisive. The same domain — Docker, Kubernetes,
Terraform, Caddy, Vault, Consul, Nomad, Portainer's backend, Grafana,
Loki — converged on Go for the same reasons. Rust's CPU edge does not
apply, and its async story slows OSS contribution velocity.

### Why React + shadcn/ui + Vite (not Next.js)

Aegis is an authenticated dashboard. No SEO, no SSR needs. Next.js
adds complexity (the App Router, server components, RSC streaming) we
do not benefit from. Vite-built SPA is simpler, faster to develop,
embedable in the Go binary via `//go:embed` for single-binary deploys.

shadcn/ui is the de-facto standard for serious React dashboards in
2025–2026 because components are copy-pasted into your repo, not
versioned as a dependency. Total customization, zero version-pin
churn. The `satnaing/shadcn-admin` scaffold is MIT-licensed and
provides ~2 weeks of frontend chrome (auth pages, sidebar, dark mode,
command palette) we would otherwise build by hand.

### Why a native agent (not a containerized agent)

This is the most debated decision. The user-visible install
experience is **identical** whichever choice we make — the controller
SSHes into the new server and sets everything up automatically; the
operator clicks "Add server" and waits 60 seconds. The difference is
what the agent runs as on the managed host.

**Native agent (chosen):**
- Single Go static binary in `/usr/local/bin/aegis-agent`.
- Runs as the `aegis` system user via a systemd unit.
- Has a narrow sudoers file with explicit allow-listed helper scripts
  (e.g. `useradd_site`, `nginx_reload`, `certbot_run`). Each helper
  takes structured arguments and validates them; the agent never
  passes user input directly to a shell.
- Can `systemctl reload nginx`, `useradd site_42`, `chown -R site_42`,
  `certbot certonly`, `docker run --label aegis.site_id=42`.

**Containerized agent (rejected):**
- Would need `--privileged` or a long list of capabilities.
- Would bind-mount `/etc/passwd`, `/etc/shadow`, `/etc/systemd/system`,
  `/etc/nginx`, `/var/run/docker.sock`, `/home`, `/srv` from the host.
- A bug in the agent then has **more access** than the native version,
  not less: the agent container can mount any host path and break out.
- Exactly the security smell Aegis exists to avoid.

The native agent is installed and updated by the controller. Operators
never run `apt install` or `wget some-installer.sh` by hand.

### Why outbound-only agent connection

Same reason Komodo v2 added it and Tailscale built a business on it:
managed servers should not expose management ports. The agent dials
out, presents its certificate, and the controller authenticates and
opens an RPC stream. NAT, firewalls, and "we forgot to lock down port
8120 on the public interface" are all neutralized.

### Why mTLS (not bearer tokens)

Bearer tokens leak. Mutual TLS with per-agent certificates means a
stolen agent binary is useless without its certificate, and the
certificate can be revoked centrally. The controller runs an internal
CA, issues certs per agent on first bootstrap, and stores fingerprints
for revocation.

### Why PostgreSQL + Redis (not just one)

- **PostgreSQL** is the source of truth for all state: users, servers,
  sites, sessions, audit log, job queue (via river). It must survive
  restarts and never lose data.
- **Redis** is for ephemeral state: WebSocket pub/sub (fan-out of live
  events to connected browsers), rate-limit counters, short-lived
  caches. If Redis dies, the app degrades but does not lose state.

Splitting these means we can scale them independently and reason about
data loss boundaries clearly.

### Why river (not asynq / sidekiq-style Redis workers)

river is Postgres-backed. Jobs persist in the same transactional store
as the rest of the data. If a deploy job is enqueued in the same
transaction that creates a site row, both commit or both don't — no
"job ran but the site doesn't exist" race. Asynq on Redis cannot offer
this without two-phase commit hacks. For destructive infrastructure
operations, transactional job enqueue is a hard requirement.

---

## 4. Component breakdown

### Controller

```
cmd/controller/main.go         — entrypoint, wiring
internal/api/                  — HTTP handlers, request/response types
internal/api/middleware/       — auth, RBAC, audit, rate limiting
internal/auth/                 — login, sessions, argon2id, JWT
internal/db/                   — sqlc-generated query bindings
internal/db/migrations/        — goose migrations (SQL)
internal/agentbus/             — per-agent connection lifecycle, RPC
internal/ca/                   — internal CA: cert issuance, revocation
internal/jobs/                 — river job definitions
internal/realtime/             — WebSocket hub, channel auth
internal/audit/                — append-only audit logger
internal/server/               — domain model: Server resource
internal/site/                 — domain model: Site resource
internal/deploy/               — per-site deploy scripts: storage, versioning,
                                 execution dispatch, webhook intake, live log
                                 streaming. Never executes a script directly —
                                 always delegates to the agent via RPC.
internal/user/                 — domain model: User, Team, Role
internal/config/               — env-driven config struct
web/                           — React + Vite + shadcn frontend (built into //go:embed)
```

### Agent

```
cmd/agent/main.go              — entrypoint
internal/agent/dialer/         — outbound mTLS connection, reconnect/backoff
internal/agent/rpc/            — handles RPCs from controller
internal/agent/host/           — host primitives: useradd, chown, nginx, certbot
internal/agent/exec/           — script execution wrapper: drops to site user via
                                 sudo, restricts cwd, scrubs env to site allow-list,
                                 captures stdout/stderr line-by-line with timestamps,
                                 enforces wall-clock + memory limits
internal/agent/docker/         — docker daemon wrapper, socket-proxy management
internal/agent/pty/            — PTY allocation for web terminal
internal/agent/metrics/        — local metric collection, push to controller
```

### Shared

```
pkg/protocol/                  — wire protocol structs (RPC messages, events)
pkg/version/                   — build-time version info
```

---

## 5. Data model (Phase 0 scope)

```sql
-- The god user is just a User with role='god'. Other users have role='site_user'.
users (
    id BIGSERIAL PRIMARY KEY,
    email TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('god', 'admin', 'site_user')),
    enabled BOOLEAN NOT NULL DEFAULT false,
    mfa_secret TEXT,                       -- TOTP secret if MFA enrolled
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
)

sessions (
    id UUID PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    issued_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    refreshed_at TIMESTAMPTZ NOT NULL,
    ip INET,
    user_agent TEXT
)

servers (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    public_ip INET NOT NULL,
    ssh_user TEXT NOT NULL,                -- user used for initial bootstrap
    agent_fingerprint TEXT UNIQUE,         -- set after agent installed
    agent_last_seen TIMESTAMPTZ,
    provision_status TEXT NOT NULL,        -- pending|provisioning|ready|error
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
)

audit_log (
    id BIGSERIAL PRIMARY KEY,
    actor_user_id BIGINT REFERENCES users(id),
    actor_ip INET,
    action TEXT NOT NULL,                  -- e.g. 'server.create', 'site.deploy'
    target_type TEXT,                      -- 'server', 'site', etc.
    target_id BIGINT,
    payload JSONB,                         -- structured before/after diff
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
```

Phase 1+ adds `sites`, `site_users`, `domains`, `certificates`,
`daemons`, `databases`, `teams`, `permissions`.

---

## 6. Deployment shape

Controller stack (operator's box or VPS):

```yaml
# docker-compose.yml (will be created in Checkpoint 0.2)
services:
  aegis:
    image: ghcr.io/danialrp/aegis:latest
    ports: ["127.0.0.1:8080:8080"]   # operator fronts with their nginx/Caddy
    depends_on: [postgres, redis]
    environment:
      AEGIS_DATABASE_URL: postgres://aegis:aegis@postgres:5432/aegis?sslmode=disable
      AEGIS_REDIS_URL: redis://redis:6379/0
      # …secrets injected from .env
    volumes:
      - aegis-ca:/var/lib/aegis/ca       # internal CA material
  postgres:
    image: postgres:16
    volumes: [aegis-pg:/var/lib/postgresql/data]
    environment:
      POSTGRES_DB: aegis
      POSTGRES_USER: aegis
      POSTGRES_PASSWORD: aegis
  redis:
    image: redis:7-alpine
    volumes: [aegis-redis:/data]

volumes: { aegis-pg, aegis-redis, aegis-ca }
```

The agent is installed by the controller via SSH on first server add.
The agent binary is embedded in the controller image and served over
HTTPS at a one-time-URL during bootstrap.

---

## 7. Non-goals (for now)

- **HA / multi-controller** — single controller; restore from Postgres
  backup. HA can come later if real demand emerges.
- **Multi-tenant SaaS** — Aegis is self-hosted by design. License is
  AGPL-3.0; SaaS deployments must publish modifications.
- **Windows / non-systemd Linux** — Ubuntu 22.04+ / Debian 12+ only.
  Adding support for other systemd distros (RHEL, Alma, Rocky) is
  cheap later. Non-systemd is out of scope.
- **Hand-edit nginx UI** — Aegis generates vhosts. The operator can
  override per-site nginx config from the panel, but the panel will
  not have a free-form "edit any config file" editor.

---

## 8. Performance budget

Targets we will measure in CI from Phase 0.4 onward:

- p99 API latency for read endpoints: **< 50 ms**
- p99 API latency for write endpoints: **< 250 ms**
- WebSocket event fan-out: **< 100 ms** controller-to-browser
- Agent reconnect after network loss: **< 30 s**
- Controller cold start: **< 3 s** to ready
- Memory at idle: **controller < 256 MB · agent < 32 MB**

These are budgets, not guesses — if a change breaches them, CI fails.
