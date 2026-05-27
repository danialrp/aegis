// SPDX-License-Identifier: AGPL-3.0-or-later

// Package protocol defines the controller↔agent wire types.
//
// The shape is JSON-over-WebSocket, one message per frame, with three
// message kinds:
//
//   - Request:  caller → callee, expects a Response with the same ID
//   - Response: callee → caller, carries either Result or Error
//   - Event:    one-way notification, no ID, no response expected
//
// Either side may initiate Requests; both sides handle them.
package protocol

import (
	"encoding/json"
	"time"
)

// MessageType discriminates the three wire frames.
type MessageType string

// Message kinds on the wire. See package doc for the full semantics
// of each.
const (
	MsgRequest  MessageType = "request"
	MsgResponse MessageType = "response"
	MsgEvent    MessageType = "event"

	// Streaming — Phase 6. Bidirectional binary streams keyed by a
	// caller-chosen ID, layered on top of the same WebSocket:
	//
	//   stream_open  caller → callee  "please open <method> with <params>"
	//   stream_ready callee → caller  ack; the stream is live
	//   stream_data  either direction  payload chunk in Params (base64)
	//   stream_close either direction  optional Error in Error field
	MsgStreamOpen  MessageType = "stream_open"
	MsgStreamReady MessageType = "stream_ready"
	MsgStreamData  MessageType = "stream_data"
	MsgStreamClose MessageType = "stream_close"
)

// Message is the single wire frame. Fields are omitempty so the JSON
// stays compact and so a parsed Response doesn't carry a spurious
// Method, etc.
type Message struct {
	Type   MessageType     `json:"type"`
	ID     string          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

// Error is the failure payload returned on a Response. Codes are
// short string tags; messages are human-readable but logged, not
// shown verbatim to end users.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Sentinel error codes used by both sides.
const (
	ErrCodeUnknownMethod = "unknown_method"
	ErrCodeBadParams     = "bad_params"
	ErrCodeInternal      = "internal"
	ErrCodeTimeout       = "timeout"
)

// --- RPC: ping ---

// PingParams is the body of a `ping` request.
type PingParams struct {
	SentAt time.Time `json:"sent_at"`
}

// PongResult is the body of a successful `ping` response.
type PongResult struct {
	SentAt time.Time `json:"sent_at"` // echoed from PingParams
	PongAt time.Time `json:"pong_at"` // agent's timestamp on receipt
}

// MethodPing is the canonical method name for the echo RPC.
const MethodPing = "ping"

// --- Host primitives (controller → agent) ---

// Host primitive methods. Each shells out to a vetted helper script
// dropped by the bootstrap; the agent never assembles shell strings.
const (
	// Phase 1.2 — site lifecycle.
	MethodHostSiteUserCreate = "host.site_user_create"
	MethodHostSiteDirEnsure  = "host.site_dir_ensure"
	MethodHostSiteDelete     = "host.site_delete"

	// Phase 1.3 — nginx vhost.
	MethodHostNginxApplyVhost  = "host.nginx_apply_vhost"
	MethodHostNginxRemoveVhost = "host.nginx_remove_vhost"

	// Phase 1.5 — exec a deploy script as site_<id>.
	MethodHostSiteRunScript = "host.site_run_script"

	// Phase 2.1 — Let's Encrypt certs via certbot.
	MethodHostCertIssue  = "host.cert_issue"
	MethodHostCertRemove = "host.cert_remove"
	MethodHostCertStatus = "host.cert_status"

	// Phase 2.4 — supervisor-managed daemons.
	MethodHostDaemonWrite  = "host.daemon_write"
	MethodHostDaemonRemove = "host.daemon_remove"
	MethodHostDaemonAction = "host.daemon_action"
	MethodHostDaemonLogs   = "host.daemon_logs"

	// Phase 3 — docker compose lifecycle.
	MethodHostNginxApplyProxyVhost = "host.nginx_apply_proxy_vhost"
	MethodHostComposeWrite         = "host.compose_write"
	MethodHostComposeAction        = "host.compose_action"
	MethodHostComposePs            = "host.compose_ps"
	MethodHostComposeLogs          = "host.compose_logs"

	// Phase 4 — PHP-FPM pool + PHP fastcgi vhost.
	MethodHostPhpFpmPoolWrite    = "host.php_fpm_pool_write"
	MethodHostPhpFpmPoolRemove   = "host.php_fpm_pool_remove"
	MethodHostNginxApplyPhpVhost = "host.nginx_apply_php_vhost"

	// Phase 5 — database engines + backups.
	MethodHostMysqlDBCreate    = "host.mysql_db_create"
	MethodHostMysqlDBDrop      = "host.mysql_db_drop"
	MethodHostPostgresDBCreate = "host.postgres_db_create"
	MethodHostPostgresDBDrop   = "host.postgres_db_drop"
	MethodHostDBBackup         = "host.db_backup"
	MethodHostDBRestore        = "host.db_restore"
	MethodHostDBBackupsList    = "host.db_backups_list"

	// Phase 6 — server metrics + PTY.
	MethodHostMetrics  = "host.metrics"
	MethodHostPtyOpen  = "host.pty_open"  // stream_open variant — opens a PTY for site_<id>
	MethodHostPtyClose = "host.pty_close" // signals controller-driven teardown
)

// MetricsResult is the snapshot returned by host.metrics. Numbers are
// kept in canonical units (bytes, percent in [0..100], seconds).
type MetricsResult struct {
	CollectedAt int64       `json:"collected_at"` // unix seconds
	UptimeSec   int64       `json:"uptime_sec"`
	LoadAvg     [3]float64  `json:"load_avg"` // 1m / 5m / 15m
	CPUCount    int         `json:"cpu_count"`
	Memory      MemoryStats `json:"memory"`
	Swap        MemoryStats `json:"swap"`
	Disks       []DiskUsage `json:"disks"`
	Kernel      string      `json:"kernel,omitempty"`
}

// MemoryStats is in bytes.
type MemoryStats struct {
	Total uint64 `json:"total"`
	Used  uint64 `json:"used"`
	Free  uint64 `json:"free"`
}

// DiskUsage covers one mounted filesystem.
type DiskUsage struct {
	Mount string `json:"mount"`
	FS    string `json:"fs"`
	Total uint64 `json:"total"`
	Used  uint64 `json:"used"`
}

// PtyOpenParams names which site_<id> to spawn the shell as.
type PtyOpenParams struct {
	SiteID int64 `json:"site_id"`
	Cols   int   `json:"cols,omitempty"`
	Rows   int   `json:"rows,omitempty"`
}

// DBCreateParams covers both mysql_db_create + postgres_db_create.
// SiteID is only consumed by the mysql helper (its first argv is
// site_id; postgres's helper ignores it).
type DBCreateParams struct {
	SiteID   int64  `json:"site_id"`
	Engine   string `json:"engine"` // "mysql" | "postgres"
	Name     string `json:"name"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// DBDropParams covers both mysql_db_drop + postgres_db_drop.
type DBDropParams struct {
	Engine   string `json:"engine"`
	Name     string `json:"name"`
	Username string `json:"username"`
}

// DBBackupParams kicks off a dump.
type DBBackupParams struct {
	SiteID int64  `json:"site_id"`
	Engine string `json:"engine"`
	Name   string `json:"name"`
}

// DBBackupResult carries the absolute path of the file that was written.
type DBBackupResult struct {
	Path string `json:"path"`
}

// DBRestoreParams names a backup file (basename, no path) inside the
// site's backups directory to restore.
type DBRestoreParams struct {
	SiteID   int64  `json:"site_id"`
	Engine   string `json:"engine"`
	Name     string `json:"name"`
	Basename string `json:"basename"`
}

// DBBackupsListResult is the raw TSV output of db_backups_list. The
// controller parses one row per line.
type DBBackupsListResult struct {
	Raw string `json:"raw"`
}

// NginxApplyPhpVhostParams configures a PHP fastcgi vhost. The pool
// socket path is derived from site_id on the agent side.
type NginxApplyPhpVhostParams struct {
	SiteID     int64  `json:"site_id"`
	Domain     string `json:"domain"`
	WorkingDir string `json:"working_dir"`
}

// NginxApplyProxyVhostParams configures a reverse-proxy vhost. nginx
// listens on 80 for Domain and forwards to 127.0.0.1:ProxyPort.
type NginxApplyProxyVhostParams struct {
	SiteID    int64  `json:"site_id"`
	Domain    string `json:"domain"`
	ProxyPort int    `json:"proxy_port"`
}

// ComposeWriteParams persists a compose.yml under /srv/sites/<id>/.
type ComposeWriteParams struct {
	SiteID int64  `json:"site_id"`
	Body   string `json:"body"`
}

// ComposeActionParams drives the lifecycle.
type ComposeActionParams struct {
	SiteID int64  `json:"site_id"`
	Action string `json:"action"` // up | down | restart | pull | build
}

// ComposePsResult carries the raw json-lines output of `docker
// compose ps --format json`. The controller parses it.
type ComposePsResult struct {
	Raw string `json:"raw"`
}

// ComposeLogsParams optionally narrows logs to a single service.
type ComposeLogsParams struct {
	SiteID  int64  `json:"site_id"`
	Service string `json:"service,omitempty"`
	Lines   int    `json:"lines,omitempty"`
}

// ComposeLogsResult is the merged log tail.
type ComposeLogsResult struct {
	Output string `json:"output"`
}

// CertIssueParams is the payload of host.cert_issue.
type CertIssueParams struct {
	Domain string `json:"domain"`
	Email  string `json:"email"`
}

// DomainParams is the payload of host.cert_remove.
type DomainParams struct {
	Domain string `json:"domain"`
}

// CertStatusResult is the payload of host.cert_status. Output is the
// raw text from `certbot certificates`; the controller parses it.
type CertStatusResult struct {
	Raw string `json:"raw"`
}

// DaemonWriteParams configures a supervisor program.
type DaemonWriteParams struct {
	SiteID      int64  `json:"site_id"`
	Slug        string `json:"slug"`         // [a-z0-9-]+, used in supervisor program name
	Command     string `json:"command"`      // full command line
	AutoRestart bool   `json:"auto_restart"` // restart on non-zero exit
}

// DaemonSlugParams is the payload for remove + log RPCs.
type DaemonSlugParams struct {
	SiteID int64  `json:"site_id"`
	Slug   string `json:"slug"`
	Lines  int    `json:"lines,omitempty"` // only used by host.daemon_logs
}

// DaemonActionParams toggles the runtime state of a supervisor program.
type DaemonActionParams struct {
	SiteID int64  `json:"site_id"`
	Slug   string `json:"slug"`
	Action string `json:"action"` // "start" | "stop" | "restart"
}

// DaemonLogsResult carries the tailed log.
type DaemonLogsResult struct {
	Output string `json:"output"`
}

// SiteIDParams is the shared payload for site-targeted host RPCs that
// only need the site id.
type SiteIDParams struct {
	SiteID int64 `json:"site_id"`
}

// NginxApplyVhostParams is the payload for host.nginx_apply_vhost.
type NginxApplyVhostParams struct {
	SiteID     int64  `json:"site_id"`
	Domain     string `json:"domain"`
	WorkingDir string `json:"working_dir"`
}

// RunScriptParams is the payload for host.site_run_script.
type RunScriptParams struct {
	SiteID     int64             `json:"site_id"`
	ScriptBody string            `json:"script_body"`
	EnvVars    map[string]string `json:"env_vars,omitempty"`
}

// RunScriptResult is the response from host.site_run_script. Output
// is the merged stdout+stderr of the deploy script.
//
// Phase 1.5 returns the whole output in one go and lets the UI poll.
// A streaming variant (per-line events) is a future refinement; the
// poll-based contract above the wire stays stable.
type RunScriptResult struct {
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
}

// HostOKResult is the trivial success response from a host RPC: the
// stdout from the helper, in case the caller wants to log it. Empty
// on success is fine.
type HostOKResult struct {
	Stdout string `json:"stdout,omitempty"`
	Stderr string `json:"stderr,omitempty"`
}
