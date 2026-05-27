# Contributing to Aegis

Thank you for considering a contribution. Aegis is early — Phase 0 is
in progress as of this document — so most rules below describe the
state we are heading toward, not where we are today. They will be
enforced from the moment they are implementable.

---

## TL;DR

1. Open an issue **before** writing more than ~20 lines of code, so we
   can agree on the approach.
2. Branch from `main`, name the branch `feat/...`, `fix/...`, or
   `docs/...`.
3. Make sure `make ci` passes locally (lint, tests, build, coverage).
4. Open a PR. The template will ask you about scope, testing, and
   security implications. Please fill it in honestly.
5. Sign your commits and agree to the AGPL-3.0 license terms (see DCO
   section below).

---

## Development environment

Aegis runs entirely in Docker. You do **not** need Go, Node, PostgreSQL,
or Redis installed locally — just Docker and `make`.

```bash
git clone https://github.com/danialrp/aegis.git
cd aegis
make dev          # brings up the full dev stack: controller, postgres, redis
                  # hot-reloads Go via `air`; hot-reloads React via Vite
open http://localhost:5173   # frontend dev server with proxy to backend
```

`make dev` mounts your source tree into the container and runs
`air` for the Go side and `vite` for the React side. Edits to either
trigger a reload within a few seconds.

If you prefer running Go and Node natively (faster builds, native
debugger), `make dev-native` brings up only Postgres + Redis in
Docker, and you run the binaries yourself.

---

## Running tests

```bash
make test               # unit tests (Go stdlib + testify)
make test-integration   # integration tests (testcontainers-go boots real Postgres + Redis)
make test-e2e           # Playwright against a fully-running stack
make ci                 # everything CI runs: lint, all tests, coverage report
```

**A PR will not be merged if it reduces overall coverage** on core
packages (`internal/auth`, `internal/site`, `internal/server`,
`internal/agentbus`, `internal/ca`, agent host primitives). The
coverage gate is intentional — these packages handle privilege
boundaries and money-on-the-wire equivalents.

For UI changes, run Playwright against your branch:

```bash
make test-e2e
```

---

## Code style

### Go

- `gofumpt` + `goimports` formatting, enforced by `golangci-lint`.
- Errors are wrapped with `fmt.Errorf("context: %w", err)` — never
  swallowed silently. Returning `error` is a contract.
- `slog` for logging, with structured key/value fields. No `log.Print`,
  no `fmt.Println` in production code paths.
- Public functions on exported types must have a doc comment.
- New SQL goes through `sqlc` — no hand-written queries in handlers.
- New tables come with a `goose` migration file.
- Tests live next to the code under test: `foo.go` ↔ `foo_test.go`.

### TypeScript / React

- Strict mode TypeScript (`"strict": true`).
- Prefer functional components with hooks. No class components.
- TanStack Router file-based routes; no inline `<Route>` definitions.
- TanStack Query for all server state. No `useState` for things that
  come from the API.
- shadcn/ui components are copy-pasted into `web/src/components/ui/` —
  edit them when needed; do not wrap a wrapper around them.
- Run `pnpm typecheck && pnpm lint && pnpm test` before opening a PR.

---

## Security-sensitive contributions

Changes that touch any of the following require an extra reviewer and
a security-implications section in the PR:

- `internal/auth/`
- `internal/api/middleware/`
- `internal/ca/`
- `internal/agentbus/`
- Anything in `internal/agent/host/` that uses `sudo`, writes outside
  the site's own directory, or alters Linux user/group records.
- Anything that handles secrets (env vars, registry credentials,
  database passwords).

If your change feels security-adjacent, say so in the PR. We would
rather discuss it openly than discover a hole post-merge.

---

## Commit messages

Conventional Commits format:

```
feat(api): add /v1/sites/{id}/restart endpoint
fix(agent): retry mTLS handshake on EOF
docs(architecture): clarify per-site docker scoping
refactor(auth): extract session repo behind interface
test(ca): cover revocation list ordering
chore(deps): bump pgx to v5.7.2
```

Subject line under 70 characters; body wraps at 72. Reference issues
with `Refs: #123` or `Fixes: #123`.

---

## DCO — Developer Certificate of Origin

Every commit must be signed off:

```
git commit -s -m "fix(agent): handle mTLS reconnect storm"
```

The `-s` flag adds a `Signed-off-by` line. By doing so you certify
the [DCO](https://developercertificate.org/) — that you have the
right to submit the contribution under the project's AGPL-3.0
license.

---

## Releases

Aegis follows semantic versioning starting at v0.1.0. Pre-1.0
releases may make breaking changes between minors; the changelog
will call them out clearly.

---

## A note on scope

Aegis aims to do a small set of things very well: server provisioning,
per-site isolation, Docker site deploys, SSL, databases, monitoring,
RBAC. PRs that add adjacent features (chat, billing, multi-cloud
provisioning, Kubernetes integration, custom DSL languages, etc.) will
likely be declined. Aegis is intentionally a single-purpose tool.

If you have an idea that doesn't fit the current scope but feels
related, open a **discussion** rather than a PR — we can talk about
whether it makes sense as a separate project, a plugin, or a future
phase.

---

Thank you for reading this far. Most projects don't have contributor
guides this long, and most contributors don't read them. The fact that
you did matters.
