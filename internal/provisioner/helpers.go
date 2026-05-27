// SPDX-License-Identifier: AGPL-3.0-or-later

package provisioner

// HelperFile is one of the executable helpers we drop on the managed
// host during bootstrap. Each helper validates its own inputs so the
// agent — running as the unprivileged `aegis` user — can invoke them
// via the narrow sudoers entry without ever passing user-typed strings
// to a shell.
type HelperFile struct {
	Path string // destination on the target, e.g. /usr/local/lib/aegis/site_useradd
	Mode string // chmod octal string for `chmod`
	Body string // file contents
}

// HelperDir is where all helpers live on the managed host. Sudoers
// allow-list paths are absolute and reference this exact directory.
const HelperDir = "/usr/local/lib/aegis"

// Helpers returns the canonical helper set installed by every
// successful bootstrap. The set grows phase by phase as new host
// primitives need privilege.
//
// Each helper is intentionally small and shell-portable (POSIX sh).
// Validating inputs here keeps the sudoers entry narrow — the
// allow-list points only at the helper, not at the underlying
// `useradd` / `install` / `nginx` / `certbot` binaries with arbitrary args.
func Helpers() []HelperFile {
	return []HelperFile{
		// Phase 1.2 — site lifecycle.
		{Path: HelperDir + "/site_useradd", Mode: "0755", Body: siteUseraddScript},
		{Path: HelperDir + "/site_dirsetup", Mode: "0755", Body: siteDirsetupScript},
		{Path: HelperDir + "/site_delete", Mode: "0755", Body: siteDeleteScript},
		// Phase 1.3 — nginx vhost.
		{Path: HelperDir + "/nginx_write_vhost", Mode: "0755", Body: nginxWriteVhostScript},
		{Path: HelperDir + "/nginx_remove_vhost", Mode: "0755", Body: nginxRemoveVhostScript},
		// Phase 1.5 — exec a deploy script as site_<id>.
		{Path: HelperDir + "/site_run_script", Mode: "0755", Body: siteRunScriptScript},
		// Phase 2.1 — SSL via Let's Encrypt (certbot --nginx).
		{Path: HelperDir + "/cert_issue", Mode: "0755", Body: certIssueScript},
		{Path: HelperDir + "/cert_remove", Mode: "0755", Body: certRemoveScript},
		{Path: HelperDir + "/cert_status", Mode: "0755", Body: certStatusScript},
		// Phase 2.4 — supervisor-managed per-site daemons.
		{Path: HelperDir + "/daemon_write", Mode: "0755", Body: daemonWriteScript},
		{Path: HelperDir + "/daemon_remove", Mode: "0755", Body: daemonRemoveScript},
		{Path: HelperDir + "/daemon_action", Mode: "0755", Body: daemonActionScript},
		{Path: HelperDir + "/daemon_logs", Mode: "0755", Body: daemonLogsScript},
		// Phase 3 — docker compose lifecycle.
		{Path: HelperDir + "/nginx_write_proxy_vhost", Mode: "0755", Body: nginxWriteProxyVhostScript},
		{Path: HelperDir + "/compose_write", Mode: "0755", Body: composeWriteScript},
		{Path: HelperDir + "/compose_action", Mode: "0755", Body: composeActionScript},
		{Path: HelperDir + "/compose_ps", Mode: "0755", Body: composePsScript},
		{Path: HelperDir + "/compose_logs", Mode: "0755", Body: composeLogsScript},
	}
}

// SudoersBody is the content written to /etc/sudoers.d/aegis. Allows
// the unprivileged `aegis` user to invoke each helper as root without
// a password, and nothing else.
const SudoersBody = `# Aegis agent — narrow sudoers allow-list.
# Managed by aegis-controller — do not edit by hand.
aegis ALL=(root) NOPASSWD: /usr/local/lib/aegis/site_useradd
aegis ALL=(root) NOPASSWD: /usr/local/lib/aegis/site_dirsetup
aegis ALL=(root) NOPASSWD: /usr/local/lib/aegis/site_delete
aegis ALL=(root) NOPASSWD: /usr/local/lib/aegis/nginx_write_vhost
aegis ALL=(root) NOPASSWD: /usr/local/lib/aegis/nginx_remove_vhost
aegis ALL=(root) NOPASSWD: /usr/local/lib/aegis/site_run_script
aegis ALL=(root) NOPASSWD: /usr/local/lib/aegis/cert_issue
aegis ALL=(root) NOPASSWD: /usr/local/lib/aegis/cert_remove
aegis ALL=(root) NOPASSWD: /usr/local/lib/aegis/cert_status
aegis ALL=(root) NOPASSWD: /usr/local/lib/aegis/daemon_write
aegis ALL=(root) NOPASSWD: /usr/local/lib/aegis/daemon_remove
aegis ALL=(root) NOPASSWD: /usr/local/lib/aegis/daemon_action
aegis ALL=(root) NOPASSWD: /usr/local/lib/aegis/daemon_logs
aegis ALL=(root) NOPASSWD: /usr/local/lib/aegis/nginx_write_proxy_vhost
aegis ALL=(root) NOPASSWD: /usr/local/lib/aegis/compose_write
aegis ALL=(root) NOPASSWD: /usr/local/lib/aegis/compose_action
aegis ALL=(root) NOPASSWD: /usr/local/lib/aegis/compose_ps
aegis ALL=(root) NOPASSWD: /usr/local/lib/aegis/compose_logs
Defaults!/usr/local/lib/aegis/* !requiretty
`

// BootstrapAptPackages lists the host packages Aegis depends on. The
// SSH bootstrap installs them via `apt-get install -y` in one step.
// Idempotent — apt no-ops if a package is already present.
var BootstrapAptPackages = []string{
	"nginx",
	"certbot",
	"python3-certbot-nginx",
	"supervisor",
	// Phase 3: docker engine + compose v2 plugin.
	// Ubuntu 22.04+/Debian 12+ ship docker.io + docker-compose-v2.
	"docker.io",
	"docker-compose-v2",
}

const siteUseraddScript = `#!/bin/sh
# Aegis helper: create site_<id> system user. Idempotent.
# Args: <site_id>  (positive integer)
set -eu

site_id="${1:-}"
case "$site_id" in
  ''|*[!0-9]*) echo "site_id must be numeric" >&2; exit 2 ;;
esac
if [ "$site_id" -lt 1 ] || [ "$site_id" -gt 1000000 ]; then
  echo "site_id out of range" >&2; exit 2
fi

user="site_$site_id"
if id -u "$user" >/dev/null 2>&1; then
  exit 0
fi

useradd \
  --system \
  --shell /usr/sbin/nologin \
  --home "/srv/sites/$site_id" \
  --no-create-home \
  "$user"
`

const siteDirsetupScript = `#!/bin/sh
# Aegis helper: lay out /srv/sites/<id> with mode-0700 ownership by
# site_<id>. Idempotent.
# Args: <site_id>  (positive integer)
set -eu

site_id="${1:-}"
case "$site_id" in
  ''|*[!0-9]*) echo "site_id must be numeric" >&2; exit 2 ;;
esac

user="site_$site_id"
dir="/srv/sites/$site_id"

install -d -m 0755 /srv/sites
install -d -o "$user" -g "$user" -m 0700 "$dir"
install -d -o "$user" -g "$user" -m 0750 "$dir/public_html"
`

const siteDeleteScript = `#!/bin/sh
# Aegis helper: tear down a site's user and directory. Best-effort —
# leftover state is preferable to a failed teardown.
# Args: <site_id>  (positive integer)
set -eu

site_id="${1:-}"
case "$site_id" in
  ''|*[!0-9]*) echo "site_id must be numeric" >&2; exit 2 ;;
esac

user="site_$site_id"
dir="/srv/sites/$site_id"

if id -u "$user" >/dev/null 2>&1; then
  userdel "$user" 2>/dev/null || true
fi
rm -rf "$dir" 2>/dev/null || true
`

const nginxWriteVhostScript = `#!/bin/sh
# Aegis helper: write /etc/nginx/sites-available/aegis-site-<id>.conf,
# symlink it into sites-enabled, validate the config, and reload.
# Args: <site_id> <domain> <working_dir>
set -eu

site_id="${1:-}"
domain="${2:-}"
working_dir="${3:-}"

case "$site_id" in
  ''|*[!0-9]*) echo "site_id must be numeric" >&2; exit 2 ;;
esac

# Allow only lowercase letters, digits, dots, hyphens. Reject leading
# dot, double-dot, edge-hyphen forms.
case "$domain" in
  '') echo "domain required" >&2; exit 2 ;;
  *[!a-z0-9.-]*) echo "domain has invalid chars" >&2; exit 2 ;;
  .*|*..*|*-.*|*.-*) echo "domain is malformed" >&2; exit 2 ;;
esac

# working_dir must equal /srv/sites/<site_id> exactly — defense in
# depth against a misbehaving controller.
expected="/srv/sites/$site_id"
case "$working_dir" in
  "$expected") : ;;
  *) echo "working_dir must equal $expected" >&2; exit 2 ;;
esac

vhost="/etc/nginx/sites-available/aegis-site-$site_id.conf"
enabled="/etc/nginx/sites-enabled/aegis-site-$site_id.conf"

cat >"$vhost" <<EOF
# Aegis-managed vhost for site $site_id — do not edit by hand.
server {
    listen 80;
    listen [::]:80;
    server_name $domain;

    root $working_dir/public_html;
    index index.html;

    location / {
        try_files \$uri \$uri/ =404;
    }

    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header Referrer-Policy "strict-origin-when-cross-origin" always;

    access_log /var/log/nginx/aegis-site-$site_id-access.log;
    error_log /var/log/nginx/aegis-site-$site_id-error.log;
}
EOF

ln -sf "$vhost" "$enabled"
nginx -t
systemctl reload nginx
`

const nginxRemoveVhostScript = `#!/bin/sh
# Aegis helper: remove vhost + symlink for a site. Best-effort.
# Args: <site_id>
set -eu

site_id="${1:-}"
case "$site_id" in
  ''|*[!0-9]*) echo "site_id must be numeric" >&2; exit 2 ;;
esac

rm -f "/etc/nginx/sites-enabled/aegis-site-$site_id.conf"
rm -f "/etc/nginx/sites-available/aegis-site-$site_id.conf"

# Reload nginx, but only if it's currently happy — don't block teardown
# on an unrelated misconfig.
if nginx -t 2>/dev/null; then
  systemctl reload nginx
fi
`

const siteRunScriptScript = `#!/bin/sh
# Aegis helper: run a deploy script as site_<id>, with cwd set to the
# site's working dir and the supplied environment loaded.
# Args: <site_id> <script_path> <env_file_path>
#
# Streams stdout+stderr unmolested to the caller's stdout. The agent
# captures and forwards them line by line.
set -eu

site_id="${1:-}"
script_path="${2:-}"
env_file="${3:-}"

case "$site_id" in
  ''|*[!0-9]*) echo "site_id must be numeric" >&2; exit 2 ;;
esac

user="site_$site_id"
dir="/srv/sites/$site_id"

if ! id -u "$user" >/dev/null 2>&1; then
  echo "user $user does not exist" >&2; exit 2
fi
if [ ! -d "$dir" ]; then
  echo "dir $dir does not exist" >&2; exit 2
fi
if [ ! -r "$script_path" ]; then
  echo "script $script_path not readable" >&2; exit 2
fi

# env_file may be empty (no per-site env vars yet).
if [ -n "$env_file" ] && [ -r "$env_file" ]; then
  env_arg="--preserve-env"
  set -a
  # shellcheck disable=SC1090
  . "$env_file"
  set +a
else
  env_arg=""
fi

# Drop privileges to the site user, change directory, run bash.
# 2>&1 merges streams so the caller gets a single ordered stdout.
exec sudo -u "$user" -H $env_arg --set-home -- \
  bash -c "cd '$dir' && exec bash '$script_path' 2>&1"
`

const certIssueScript = `#!/bin/sh
# Aegis helper: issue a Let's Encrypt cert via certbot --nginx for the
# given domain. nginx must already be serving the domain on port 80
# (the HTTP-01 challenge is over port 80).
# Args: <domain> <email>
set -eu

domain="${1:-}"
email="${2:-}"

case "$domain" in
  '') echo "domain required" >&2; exit 2 ;;
  *[!a-z0-9.-]*) echo "domain has invalid chars" >&2; exit 2 ;;
esac
case "$email" in
  '') echo "email required" >&2; exit 2 ;;
  *[!a-zA-Z0-9.+_@-]*) echo "email has invalid chars" >&2; exit 2 ;;
esac

# --non-interactive + --agree-tos for unattended use.
# --redirect rewrites the port-80 vhost to 301 → HTTPS.
# --no-eff-email skips the EFF newsletter prompt.
exec certbot --nginx --non-interactive --agree-tos --no-eff-email \
  --email "$email" --redirect --domain "$domain"
`

const certRemoveScript = `#!/bin/sh
# Aegis helper: revoke + delete a Let's Encrypt cert for the given
# domain. Best effort.
# Args: <domain>
set -eu

domain="${1:-}"
case "$domain" in
  '') echo "domain required" >&2; exit 2 ;;
  *[!a-z0-9.-]*) echo "domain has invalid chars" >&2; exit 2 ;;
esac

certbot delete --non-interactive --cert-name "$domain" 2>/dev/null || true
`

const certStatusScript = `#!/bin/sh
# Aegis helper: print certbot's machine-readable cert state. Output is
# parsed by the agent.
set -eu
exec certbot certificates 2>/dev/null
`

const daemonWriteScript = `#!/bin/sh
# Aegis helper: write a supervisor program config for a site daemon
# and reload supervisor. Idempotent.
# Args: <site_id> <slug> <command> <auto_restart>
#   <slug> = [a-z0-9-]+, used in the supervisor program name
#   <auto_restart> = "true" | "false"
set -eu

site_id="${1:-}"
slug="${2:-}"
command_str="${3:-}"
auto_restart="${4:-}"

case "$site_id" in ''|*[!0-9]*) echo "site_id must be numeric" >&2; exit 2 ;; esac
case "$slug" in
  '') echo "slug required" >&2; exit 2 ;;
  *[!a-z0-9-]*) echo "slug must be [a-z0-9-]+" >&2; exit 2 ;;
esac
case "$auto_restart" in
  true|false) : ;;
  *) echo "auto_restart must be true|false" >&2; exit 2 ;;
esac
if [ -z "$command_str" ]; then
  echo "command required" >&2; exit 2
fi

user="site_$site_id"
dir="/srv/sites/$site_id"
name="aegis-site-$site_id-$slug"
conf="/etc/supervisor/conf.d/$name.conf"

if ! id -u "$user" >/dev/null 2>&1; then
  echo "user $user does not exist" >&2; exit 2
fi

cat >"$conf" <<EOF
# Aegis-managed daemon $name — do not edit by hand.
[program:$name]
command=$command_str
directory=$dir
user=$user
autostart=true
autorestart=$auto_restart
stopasgroup=true
killasgroup=true
stdout_logfile=/var/log/supervisor/$name.log
stderr_logfile=/var/log/supervisor/$name-err.log
stdout_logfile_maxbytes=10MB
stdout_logfile_backups=3
EOF

supervisorctl reread
supervisorctl update "$name"
`

const daemonRemoveScript = `#!/bin/sh
# Aegis helper: stop + remove a supervisor program. Best effort.
# Args: <site_id> <slug>
set -eu

site_id="${1:-}"
slug="${2:-}"
case "$site_id" in ''|*[!0-9]*) echo "site_id must be numeric" >&2; exit 2 ;; esac
case "$slug" in '') exit 2 ;; *[!a-z0-9-]*) exit 2 ;; esac

name="aegis-site-$site_id-$slug"
conf="/etc/supervisor/conf.d/$name.conf"

supervisorctl stop "$name" 2>/dev/null || true
rm -f "$conf"
supervisorctl reread
supervisorctl update
`

const daemonActionScript = `#!/bin/sh
# Aegis helper: start / stop / restart a supervisor program.
# Args: <site_id> <slug> <action>
set -eu

site_id="${1:-}"
slug="${2:-}"
action="${3:-}"
case "$site_id" in ''|*[!0-9]*) exit 2 ;; esac
case "$slug" in '') exit 2 ;; *[!a-z0-9-]*) exit 2 ;; esac
case "$action" in
  start|stop|restart) : ;;
  *) echo "action must be start|stop|restart" >&2; exit 2 ;;
esac

exec supervisorctl "$action" "aegis-site-$site_id-$slug"
`

const daemonLogsScript = `#!/bin/sh
# Aegis helper: tail a supervisor program's stdout log.
# Args: <site_id> <slug> <lines>
set -eu

site_id="${1:-}"
slug="${2:-}"
lines="${3:-200}"
case "$site_id" in ''|*[!0-9]*) exit 2 ;; esac
case "$slug" in '') exit 2 ;; *[!a-z0-9-]*) exit 2 ;; esac
case "$lines" in ''|*[!0-9]*) lines=200 ;; esac
if [ "$lines" -gt 5000 ]; then lines=5000; fi

name="aegis-site-$site_id-$slug"
log="/var/log/supervisor/$name.log"
err="/var/log/supervisor/$name-err.log"

if [ -r "$log" ]; then
  tail -n "$lines" "$log"
fi
if [ -r "$err" ]; then
  echo "----- stderr -----"
  tail -n "$lines" "$err"
fi
`

const nginxWriteProxyVhostScript = `#!/bin/sh
# Aegis helper: write a reverse-proxy nginx vhost for a docker-type
# site. nginx → 127.0.0.1:<proxy_port>.
# Args: <site_id> <domain> <proxy_port>
set -eu

site_id="${1:-}"
domain="${2:-}"
proxy_port="${3:-}"

case "$site_id" in ''|*[!0-9]*) echo "site_id must be numeric" >&2; exit 2 ;; esac
case "$domain" in
  '') echo "domain required" >&2; exit 2 ;;
  *[!a-z0-9.-]*) echo "domain has invalid chars" >&2; exit 2 ;;
  .*|*..*|*-.*|*.-*) echo "domain is malformed" >&2; exit 2 ;;
esac
case "$proxy_port" in
  ''|*[!0-9]*) echo "proxy_port must be numeric" >&2; exit 2 ;;
esac
if [ "$proxy_port" -lt 1 ] || [ "$proxy_port" -gt 65535 ]; then
  echo "proxy_port out of range" >&2; exit 2
fi

vhost="/etc/nginx/sites-available/aegis-site-$site_id.conf"
enabled="/etc/nginx/sites-enabled/aegis-site-$site_id.conf"

cat >"$vhost" <<EOF
# Aegis-managed proxy vhost for site $site_id — do not edit by hand.
upstream aegis_site_$site_id {
    server 127.0.0.1:$proxy_port;
    keepalive 16;
}

server {
    listen 80;
    listen [::]:80;
    server_name $domain;

    location / {
        proxy_pass http://aegis_site_$site_id;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
    }

    access_log /var/log/nginx/aegis-site-$site_id-access.log;
    error_log /var/log/nginx/aegis-site-$site_id-error.log;
}
EOF

ln -sf "$vhost" "$enabled"
nginx -t
systemctl reload nginx
`

const composeWriteScript = `#!/bin/sh
# Aegis helper: write /srv/sites/<id>/compose.yml from stdin, owned by
# site_<id>:site_<id>, mode 0640.
# Args: <site_id>
# Stdin: the compose file body.
set -eu

site_id="${1:-}"
case "$site_id" in ''|*[!0-9]*) echo "site_id must be numeric" >&2; exit 2 ;; esac

user="site_$site_id"
dir="/srv/sites/$site_id"

if ! id -u "$user" >/dev/null 2>&1; then
  echo "user $user does not exist" >&2; exit 2
fi
if [ ! -d "$dir" ]; then
  echo "dir $dir does not exist" >&2; exit 2
fi

tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
cat >"$tmp"
install -o "$user" -g "$user" -m 0640 "$tmp" "$dir/compose.yml"
`

const composeActionScript = `#!/bin/sh
# Aegis helper: run docker compose lifecycle commands against
# /srv/sites/<id>/compose.yml with project name aegis-site-<id>.
# Args: <site_id> <action>   action in {up, down, restart, pull, build}
set -eu

site_id="${1:-}"
action="${2:-}"
case "$site_id" in ''|*[!0-9]*) echo "site_id must be numeric" >&2; exit 2 ;; esac
case "$action" in
  up|down|restart|pull|build) : ;;
  *) echo "invalid action" >&2; exit 2 ;;
esac

dir="/srv/sites/$site_id"
project="aegis-site-$site_id"

if [ ! -r "$dir/compose.yml" ]; then
  echo "no compose.yml at $dir" >&2; exit 2
fi

cd "$dir"
case "$action" in
  up)      exec docker compose -p "$project" up -d --remove-orphans ;;
  down)    exec docker compose -p "$project" down ;;
  restart) exec docker compose -p "$project" restart ;;
  pull)    exec docker compose -p "$project" pull ;;
  build)   exec docker compose -p "$project" build ;;
esac
`

const composePsScript = `#!/bin/sh
# Aegis helper: list containers for site <id>'s compose project as
# JSON (one object per line — docker compose's --format json).
# Args: <site_id>
set -eu

site_id="${1:-}"
case "$site_id" in ''|*[!0-9]*) exit 2 ;; esac

dir="/srv/sites/$site_id"
project="aegis-site-$site_id"

if [ ! -r "$dir/compose.yml" ]; then
  exit 0   # no compose yet → empty output, not error
fi

cd "$dir"
exec docker compose -p "$project" ps --all --format json
`

const composeLogsScript = `#!/bin/sh
# Aegis helper: tail logs for the whole compose project (or a single
# service).
# Args: <site_id> <service-or-empty> <lines>
set -eu

site_id="${1:-}"
service="${2:-}"
lines="${3:-200}"

case "$site_id" in ''|*[!0-9]*) exit 2 ;; esac
case "$lines" in ''|*[!0-9]*) lines=200 ;; esac
if [ "$lines" -gt 5000 ]; then lines=5000; fi
case "$service" in
  *[!a-zA-Z0-9_-]*) echo "service has invalid chars" >&2; exit 2 ;;
esac

dir="/srv/sites/$site_id"
project="aegis-site-$site_id"
if [ ! -r "$dir/compose.yml" ]; then
  exit 0
fi
cd "$dir"

if [ -z "$service" ]; then
  exec docker compose -p "$project" logs --tail "$lines" --timestamps
else
  exec docker compose -p "$project" logs --tail "$lines" --timestamps -- "$service"
fi
`
