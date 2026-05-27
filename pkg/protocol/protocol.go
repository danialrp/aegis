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
