// SPDX-License-Identifier: AGPL-3.0-or-later

package agentbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"

	"github.com/danialrp/aegis/pkg/protocol"
)

// Default deadlines. Both sides should set these symmetrically.
const (
	writeTimeout   = 10 * time.Second
	requestTimeout = 30 * time.Second
)

// Conn is one live agent WebSocket connection. Owns a read goroutine
// and a write-serialization mutex; serves both inbound RPCs from the
// agent (none in 0.7) and outbound requests from the controller.
type Conn struct {
	serverID int64
	ws       *websocket.Conn
	logger   *slog.Logger

	writeMu sync.Mutex // serializes wsjson.Write calls

	pendingMu sync.Mutex
	pending   map[string]chan *protocol.Message
}

func newConn(ws *websocket.Conn, serverID int64, logger *slog.Logger) *Conn {
	return &Conn{
		serverID: serverID,
		ws:       ws,
		logger:   logger.With("server_id", serverID),
		pending:  make(map[string]chan *protocol.Message),
	}
}

// ServerID returns the verified server id derived from the agent's
// mTLS client cert at handshake time.
func (c *Conn) ServerID() int64 { return c.serverID }

// Request sends a JSON-RPC-style request to the agent and waits for a
// matching response. ctx cancels the wait; on timeout, the pending
// entry is cleaned up but the underlying WS stays open.
func (c *Conn) Request(ctx context.Context, method string, params any) (*protocol.Message, error) {
	id := uuid.NewString()

	var paramsRaw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		paramsRaw = b
	}

	respCh := make(chan *protocol.Message, 1)
	c.pendingMu.Lock()
	c.pending[id] = respCh
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	msg := protocol.Message{
		Type:   protocol.MsgRequest,
		ID:     id,
		Method: method,
		Params: paramsRaw,
	}
	if err := c.writeMessage(ctx, msg); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-respCh:
		if resp.Error != nil {
			return resp, fmt.Errorf("rpc %s: %s: %s", method, resp.Error.Code, resp.Error.Message)
		}
		return resp, nil
	}
}

// readLoop runs until the WebSocket closes. Returns the close cause.
func (c *Conn) readLoop(ctx context.Context) error {
	for {
		var msg protocol.Message
		if err := wsjson.Read(ctx, c.ws, &msg); err != nil {
			return err
		}

		switch msg.Type {
		case protocol.MsgResponse:
			c.routeResponse(&msg)
		case protocol.MsgRequest:
			// 0.7 does not define any agent-initiated RPCs. Reject so
			// a misbehaving agent surfaces clearly rather than hanging.
			resp := protocol.Message{
				Type: protocol.MsgResponse,
				ID:   msg.ID,
				Error: &protocol.Error{
					Code:    protocol.ErrCodeUnknownMethod,
					Message: "controller has no agent-initiated RPCs in this phase",
				},
			}
			if err := c.writeMessage(ctx, resp); err != nil {
				return err
			}
		case protocol.MsgEvent:
			c.logger.Debug("event from agent", "method", msg.Method)
		default:
			c.logger.Warn("unknown message type", "type", string(msg.Type))
		}
	}
}

func (c *Conn) routeResponse(msg *protocol.Message) {
	c.pendingMu.Lock()
	ch, ok := c.pending[msg.ID]
	c.pendingMu.Unlock()
	if !ok {
		c.logger.Warn("response for unknown request id", "id", msg.ID)
		return
	}
	select {
	case ch <- msg:
	default:
		// Buffered chan size 1; if it's full the caller already moved on.
	}
}

func (c *Conn) writeMessage(ctx context.Context, msg protocol.Message) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return wsjson.Write(ctx, c.ws, msg)
}

// close shuts the WS down cleanly.
func (c *Conn) close(status websocket.StatusCode, reason string) {
	_ = c.ws.Close(status, reason)
	c.failPending(errors.New("connection closed"))
}

func (c *Conn) failPending(err error) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for id, ch := range c.pending {
		select {
		case ch <- &protocol.Message{
			Type: protocol.MsgResponse,
			ID:   id,
			Error: &protocol.Error{
				Code:    protocol.ErrCodeInternal,
				Message: err.Error(),
			},
		}:
		default:
		}
	}
}
