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
// `useradd` / `install` / `nginx` binaries with arbitrary args.
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
Defaults!/usr/local/lib/aegis/* !requiretty
`

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
