# Security Policy

Aegis manages production Linux servers, deploys code, holds SSH and
TLS material, and bridges to Docker. Security is the project's reason
for existing. We take vulnerability reports seriously and respond
quickly.

---

## Reporting a vulnerability

**Please do not file public GitHub issues for security bugs.**

Report vulnerabilities privately via one of the following:

- **Preferred:** GitHub Security Advisories — open a private advisory
  at https://github.com/danialrp/aegis/security/advisories/new
- **Email:** open a public issue asking for a contact email, and we
  will reply with a PGP key and address. _(Aegis is a young project;
  once a stable contact is established, this section will be updated.)_

Please include:

- A clear description of the issue, the impact, and any preconditions.
- A reproducer if possible (PoC code, commands, configuration).
- The Aegis version (`aegis-controller --version` and `aegis-agent --version`).
- Whether you would like public credit in the advisory.

We aim to acknowledge reports within **3 working days** and to ship a
fix or mitigation within **30 days** for high-severity issues. If a
report requires longer, we will keep you informed of progress.

We will not pursue legal action against good-faith researchers who
follow this disclosure process and do not exfiltrate data beyond
what's needed to demonstrate the issue.

---

## Supported versions

Aegis is in pre-release development. Until v1.0:

- Only the latest tagged release is supported.
- Security fixes will be applied to `main` and a patch release cut.

After v1.0, this section will document an N-1 support window.

---

## Security model

The full design rationale is in [ARCHITECTURE.md](./ARCHITECTURE.md).
A brief summary of what Aegis promises and what it does **not** promise:

### What Aegis defends against

- **Cross-site filesystem reads** — site users cannot read other sites'
  files, even via the panel's embedded terminal. Enforced at the OS
  level: site directories are mode `700` owned by `site_<id>`.
- **Cross-site process / Docker access** — site users cannot manage,
  inspect, or read logs from other sites' containers via Docker CLI or
  the panel UI. Enforced via per-site docker-socket-proxy filtering on
  the `aegis.site_id` label.
- **Inbound network exposure on managed servers** — the agent connects
  outbound to the controller. No Aegis-related inbound port is opened
  on a managed server.
- **Replay of controller↔agent messages** — mTLS with per-agent certs;
  the controller's CA can revoke a single agent's cert without
  impacting other agents.
- **Privilege escalation via the panel** — there is no UI flow by which
  a site user can request more privilege. Elevation is a deliberate
  action by a god user, audit-logged.
- **Credential exposure in logs** — secrets injected into deploys
  (env vars, registry passwords) are referenced by ID, never written
  to job logs or audit payloads.

### What Aegis does **not** defend against

- **Root compromise on a managed server.** If the OS itself is rooted,
  all bets are off. Aegis is one layer; the OS hardening, kernel
  patching, and physical/cloud-provider security are yours.
- **Compromise of the controller host.** A controller compromise
  yields access to every managed server's agent certificate. Treat
  the controller box like a Tier-0 asset: separate from any tenant
  workload, locked down, MFA on SSH, etc.
- **A god user with a weak password.** No software can protect a
  superuser account from a credential leak. Use a long random
  password and enable MFA (Phase 7+).
- **Container escapes.** Aegis runs sites' Docker workloads on a
  shared kernel; a kernel-level container escape would bypass
  per-site Docker scoping. For workloads that require kernel-level
  isolation, use VMs (Firecracker, Kata) — Aegis does not provide
  this.
- **Side-channel attacks** — CPU/cache/microarch sidechannels are out
  of scope. Use SMT-disabled hardware or hypervisor isolation for
  workloads that require it.

---

## Threat model

The intended operating environment:

- **Operator (god user):** trusted, holds all keys to the kingdom.
- **Site users (developers):** semi-trusted. They have legitimate
  access to their site, may have shell access to it, and must NOT
  be able to reach other sites.
- **Anonymous internet:** untrusted. Reaches sites over HTTPS but
  never reaches the Aegis controller or agent without authentication.
- **Tenant code:** untrusted. May be a freshly cloned WordPress with
  three vulnerable plugins or a half-deployed Laravel app. Aegis
  assumes it. Per-site isolation contains the blast radius.

---

## Cryptography

- **Passwords** — argon2id with parameters tuned for ~100 ms hashing
  on commodity x86 hardware. Periodic re-hashing on login when
  parameters are bumped.
- **Session tokens** — short-lived JWTs (15 min access, 30 day refresh
  in a rotating session row).
- **Controller↔agent** — mutual TLS. Agents present per-agent
  certificates issued by the controller's internal CA. Cipher suites
  restricted to TLS 1.3 AEAD suites.
- **At-rest secrets** — env vars and tokens stored in the controller's
  Postgres are encrypted with a master key (`AEGIS_SECRET_KEY`).
  Operators are responsible for protecting `AEGIS_SECRET_KEY`.

---

## Hardening checklist for operators

When Aegis reaches a production-ready release, this section will list
the concrete checks an operator should run before exposing a
controller to the internet. Until then: keep the controller behind a
trusted network or a VPN, and don't put it on the public internet.
