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

// The three message kinds on the wire. See package doc for the full
// semantics of each.
const (
	MsgRequest  MessageType = "request"
	MsgResponse MessageType = "response"
	MsgEvent    MessageType = "event"
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
)

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
