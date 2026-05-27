// SPDX-License-Identifier: AGPL-3.0-or-later

// Package agentbus is the controller-side runtime for connected
// agents. It owns:
//
//   - the HTTP handler that upgrades incoming agent dials to
//     WebSocket, after mTLS verification + server-id extraction
//   - the in-memory registry of currently-connected agents
//   - helpers for the controller to send RPCs to specific agents
package agentbus

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"

	"github.com/coder/websocket"

	"github.com/danialrp/aegis/internal/ca"
	"github.com/danialrp/aegis/internal/db/sqlc"
)

// ErrNoAgent is returned when the controller tries to talk to a
// server id that has no live connection right now.
var ErrNoAgent = errors.New("no live agent for server")

// Hub keeps the controller's view of connected agents.
type Hub struct {
	logger  *slog.Logger
	queries *sqlc.Queries

	mu     sync.RWMutex
	agents map[int64]*Conn
}

// NewHub builds a Hub. queries is used to bump servers.agent_last_seen
// on each successful handshake; pass nil only in tests where you
// don't care about persistence.
func NewHub(logger *slog.Logger, queries *sqlc.Queries) *Hub {
	return &Hub{
		logger:  logger,
		queries: queries,
		agents:  make(map[int64]*Conn),
	}
}

// Get returns the live connection for serverID, or (nil, false).
func (h *Hub) Get(serverID int64) (*Conn, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	c, ok := h.agents[serverID]
	return c, ok
}

// Count reports how many agents are connected right now.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.agents)
}

// Handler returns the http.Handler to mount at /agent/v1/ws on the
// agent-port http.Server.
//
// The handler assumes the surrounding *http.Server was constructed
// with tls.Config{ClientAuth: tls.RequireAndVerifyClientCert} and our
// CA pool — so r.TLS.PeerCertificates is non-empty and signed by us.
func (h *Hub) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, "client certificate required", http.StatusUnauthorized)
			return
		}
		cert := r.TLS.PeerCertificates[0]

		serverID, err := ca.ExtractServerID(cert)
		if err != nil {
			h.logger.Warn("agent cert has no server id SAN",
				"subject", cert.Subject.CommonName, "err", err)
			http.Error(w, "invalid agent certificate", http.StatusUnauthorized)
			return
		}

		// Best-effort: record we saw them. A DB failure here doesn't
		// block the handshake — the controller can rebuild lastSeen
		// from logs if it ever needs to.
		if h.queries != nil {
			if err := h.queries.TouchServerAgent(r.Context(), serverID); err != nil {
				h.logger.Warn("touch server failed", "server_id", serverID, "err", err)
			}
		}

		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: []string{"*"}, // agents are not browsers
		})
		if err != nil {
			h.logger.Warn("ws accept failed", "server_id", serverID, "err", err)
			return
		}

		conn := newConn(ws, serverID, h.logger)
		if existing := h.register(serverID, conn); existing != nil {
			// A second connection from the same server displaces the
			// first — typical reconnect-storm pattern. Close the old
			// one with a known reason; the agent will treat it as a
			// clean shutdown.
			existing.close(websocket.StatusPolicyViolation, "replaced by newer connection")
		}
		defer h.deregister(serverID, conn)

		h.logger.Info("agent connected", "server_id", serverID)

		if err := conn.readLoop(r.Context()); err != nil &&
			!errors.Is(err, context.Canceled) {
			h.logger.Info("agent disconnected", "server_id", serverID, "err", err)
		}

		conn.close(websocket.StatusNormalClosure, "")
	}
}

// register inserts conn; returns the prior conn (if any) so the caller
// can close it.
func (h *Hub) register(serverID int64, conn *Conn) *Conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	prev := h.agents[serverID]
	h.agents[serverID] = conn
	return prev
}

// deregister removes conn only if it is still the current one (a
// reconnect race could have already replaced it).
func (h *Hub) deregister(serverID int64, conn *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if current, ok := h.agents[serverID]; ok && current == conn {
		delete(h.agents, serverID)
	}
}
