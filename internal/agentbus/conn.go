// SPDX-License-Identifier: AGPL-3.0-or-later

package agentbus

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	streamsMu sync.Mutex
	streams   map[string]*Stream
}

// Stream is one in-flight bidirectional stream. Created by
// OpenStream; data arrives on Recv, send via Send, finish with Close.
type Stream struct {
	ID   string
	conn *Conn

	recvCh   chan []byte
	readyCh  chan struct{} // closed when stream_ready arrives
	closeCh  chan error    // closed/sent when peer closes
	closedMu sync.Mutex
	closed   bool
}

// Recv returns the next data chunk or io.EOF when the stream ends.
func (s *Stream) Recv() ([]byte, error) {
	select {
	case b, ok := <-s.recvCh:
		if !ok {
			return nil, io.EOF
		}
		return b, nil
	case err := <-s.closeCh:
		// Drain pending data before yielding the close error so the
		// caller sees every byte the peer sent.
		select {
		case b, ok := <-s.recvCh:
			if ok {
				return b, nil
			}
		default:
		}
		if err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
}

// Send pushes a data chunk to the peer.
func (s *Stream) Send(ctx context.Context, b []byte) error {
	return s.conn.writeMessage(ctx, protocol.Message{
		Type:   protocol.MsgStreamData,
		ID:     s.ID,
		Params: encodeStreamPayload(b),
	})
}

// Close sends stream_close to the peer and tears down local state.
func (s *Stream) Close() error {
	s.closedMu.Lock()
	if s.closed {
		s.closedMu.Unlock()
		return nil
	}
	s.closed = true
	s.closedMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	_ = s.conn.writeMessage(ctx, protocol.Message{
		Type: protocol.MsgStreamClose, ID: s.ID,
	})
	s.conn.streamsMu.Lock()
	delete(s.conn.streams, s.ID)
	s.conn.streamsMu.Unlock()
	return nil
}

func newConn(ws *websocket.Conn, serverID int64, logger *slog.Logger) *Conn {
	return &Conn{
		serverID: serverID,
		ws:       ws,
		logger:   logger.With("server_id", serverID),
		pending:  make(map[string]chan *protocol.Message),
		streams:  make(map[string]*Stream),
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
		case protocol.MsgStreamReady, protocol.MsgStreamData, protocol.MsgStreamClose:
			c.routeStream(&msg)
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

// OpenStream initiates a stream RPC and returns a Stream the caller
// can Recv/Send/Close on. The Stream is ready to send/recv as soon as
// the agent acks with stream_ready (or this returns an error).
func (c *Conn) OpenStream(ctx context.Context, method string, params any) (*Stream, error) {
	id := uuid.NewString()

	var paramsRaw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		paramsRaw = b
	}

	s := &Stream{
		ID:      id,
		conn:    c,
		recvCh:  make(chan []byte, 64),
		readyCh: make(chan struct{}),
		closeCh: make(chan error, 1),
	}
	c.streamsMu.Lock()
	c.streams[id] = s
	c.streamsMu.Unlock()

	if err := c.writeMessage(ctx, protocol.Message{
		Type:   protocol.MsgStreamOpen,
		ID:     id,
		Method: method,
		Params: paramsRaw,
	}); err != nil {
		c.streamsMu.Lock()
		delete(c.streams, id)
		c.streamsMu.Unlock()
		return nil, err
	}

	// Wait for stream_ready (or close-on-error).
	select {
	case <-s.readyCh:
		return s, nil
	case err := <-s.closeCh:
		c.streamsMu.Lock()
		delete(c.streams, id)
		c.streamsMu.Unlock()
		if err == nil {
			err = errors.New("stream closed before ready")
		}
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *Conn) routeStream(msg *protocol.Message) {
	c.streamsMu.Lock()
	s, ok := c.streams[msg.ID]
	c.streamsMu.Unlock()
	if !ok {
		c.logger.Debug("stream frame for unknown id", "id", msg.ID, "type", string(msg.Type))
		return
	}
	switch msg.Type {
	case protocol.MsgStreamReady:
		select {
		case <-s.readyCh: // already closed
		default:
			close(s.readyCh)
		}
	case protocol.MsgStreamData:
		b, err := decodeStreamPayload(msg.Params)
		if err != nil {
			c.logger.Warn("decode stream payload", "err", err)
			return
		}
		select {
		case s.recvCh <- b:
		default:
			// Buffer full — drop. Terminal streams should be drained
			// quickly by the browser; if not, we'd grow unbounded.
			c.logger.Warn("stream recv buffer full, dropping chunk", "id", msg.ID)
		}
	case protocol.MsgStreamClose:
		var perr error
		if msg.Error != nil {
			perr = fmt.Errorf("%s: %s", msg.Error.Code, msg.Error.Message)
		}
		s.closedMu.Lock()
		s.closed = true
		s.closedMu.Unlock()
		select {
		case s.closeCh <- perr:
		default:
		}
		close(s.recvCh)
		c.streamsMu.Lock()
		delete(c.streams, msg.ID)
		c.streamsMu.Unlock()
	}
}

// encodeStreamPayload wraps a byte slice in a JSON object with a
// base64-encoded "b" field so it survives JSON transit.
func encodeStreamPayload(b []byte) json.RawMessage {
	enc := base64.StdEncoding.EncodeToString(b)
	out, _ := json.Marshal(map[string]string{"b": enc})
	return out
}

func decodeStreamPayload(p json.RawMessage) ([]byte, error) {
	if len(p) == 0 {
		return nil, nil
	}
	var box struct {
		B string `json:"b"`
	}
	if err := json.Unmarshal(p, &box); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(box.B)
}
