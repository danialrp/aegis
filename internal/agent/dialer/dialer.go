// SPDX-License-Identifier: AGPL-3.0-or-later

// Package dialer is the agent's outbound mTLS+WSS connection
// machinery. Run loops on the lifetime of the agent process,
// reconnecting with bounded exponential backoff whenever the WS
// drops.
package dialer

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/danialrp/aegis/internal/agent/pty"
	"github.com/danialrp/aegis/internal/agent/rpc"
	"github.com/danialrp/aegis/pkg/protocol"
)

const (
	initialBackoff = time.Second
	maxBackoff     = 30 * time.Second

	handshakeTimeout = 10 * time.Second
	writeTimeout     = 10 * time.Second
)

// Dialer owns the lifetime of one outbound WS connection. Re-creates
// it on failure.
type Dialer struct {
	url       string
	tlsConfig *tls.Config
	handler   *rpc.Handler
	pty       *pty.Manager
	logger    *slog.Logger

	// Per-connection state for streams (PTY). Reset on each reconnect.
	writeMu sync.Mutex
	ws      *websocket.Conn
}

// New builds a Dialer.
func New(url string, tlsConfig *tls.Config, handler *rpc.Handler, ptyMgr *pty.Manager, logger *slog.Logger) *Dialer {
	return &Dialer{
		url:       url,
		tlsConfig: tlsConfig,
		handler:   handler,
		pty:       ptyMgr,
		logger:    logger,
	}
}

// Run blocks until ctx is cancelled. Returns ctx.Err() on cancel;
// transient connection errors are logged and retried.
func (d *Dialer) Run(ctx context.Context) error {
	backoff := initialBackoff

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := d.serveOne(ctx)
		switch {
		case err == nil:
			// Clean disconnect — reconnect immediately, reset backoff.
			backoff = initialBackoff
		case errors.Is(err, context.Canceled):
			return err
		default:
			d.logger.Warn("agent connection error",
				"err", err, "retry_in", backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

func (d *Dialer) serveOne(ctx context.Context) error {
	dialCtx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()

	httpClient := &http.Client{
		Transport: &http.Transport{TLSClientConfig: d.tlsConfig},
	}

	ws, resp, err := websocket.Dial(dialCtx, d.url, &websocket.DialOptions{
		HTTPClient: httpClient,
	})
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	// On a successful upgrade resp.Body is already an io.Closer that
	// the websocket lib has hijacked; closing it is a no-op but
	// silences bodyclose.
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer func() { _ = ws.Close(websocket.StatusGoingAway, "shutdown") }()

	d.writeMu.Lock()
	d.ws = ws
	d.writeMu.Unlock()
	defer func() {
		d.writeMu.Lock()
		d.ws = nil
		d.writeMu.Unlock()
	}()

	d.logger.Info("connected to controller", "url", d.url)

	for {
		var msg protocol.Message
		if err := wsjson.Read(ctx, ws, &msg); err != nil {
			return fmt.Errorf("ws read: %w", err)
		}

		switch msg.Type {
		case protocol.MsgRequest:
			d.handleRequest(ctx, ws, &msg)
		case protocol.MsgStreamOpen:
			d.handleStreamOpen(ctx, ws, &msg)
		case protocol.MsgStreamData:
			d.handleStreamData(&msg)
		case protocol.MsgStreamClose:
			d.handleStreamClose(&msg)
		case protocol.MsgResponse:
			d.logger.Warn("unsolicited response from controller", "id", msg.ID)
		case protocol.MsgEvent:
			d.logger.Debug("event from controller", "method", msg.Method)
		default:
			d.logger.Warn("unknown message type", "type", string(msg.Type))
		}
	}
}

func (d *Dialer) handleRequest(ctx context.Context, ws *websocket.Conn, req *protocol.Message) {
	result, rpcErr := d.handler.Dispatch(ctx, req.Method, req.Params)

	resp := protocol.Message{
		Type: protocol.MsgResponse,
		ID:   req.ID,
	}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else if result != nil {
		b, err := json.Marshal(result)
		if err != nil {
			resp.Error = &protocol.Error{
				Code:    protocol.ErrCodeInternal,
				Message: fmt.Sprintf("marshal result: %v", err),
			}
		} else {
			resp.Result = b
		}
	}

	writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	if err := wsjson.Write(writeCtx, ws, resp); err != nil {
		d.logger.Warn("write response failed", "id", req.ID, "err", err)
	}
}

// --- streams ---

func (d *Dialer) handleStreamOpen(ctx context.Context, ws *websocket.Conn, msg *protocol.Message) {
	switch msg.Method {
	case protocol.MethodHostPtyOpen:
		if d.pty == nil {
			d.streamErr(ctx, ws, msg.ID, "pty_unavailable", "no pty manager configured")
			return
		}
		if err := d.pty.Open(ctx, msg.ID, msg.Params, dialerSink{d: d}); err != nil {
			d.streamErr(ctx, ws, msg.ID, "pty_open_failed", err.Error())
		}
	default:
		d.streamErr(ctx, ws, msg.ID, "unknown_stream_method", msg.Method)
	}
}

func (d *Dialer) handleStreamData(msg *protocol.Message) {
	if d.pty == nil {
		return
	}
	b, err := decodeDataPayload(msg.Params)
	if err != nil {
		d.logger.Warn("decode stream data", "err", err)
		return
	}
	if err := d.pty.Write(msg.ID, b); err != nil {
		d.logger.Debug("pty write", "id", msg.ID, "err", err)
	}
}

func (d *Dialer) handleStreamClose(msg *protocol.Message) {
	if d.pty == nil {
		return
	}
	d.pty.Close(msg.ID)
}

func (d *Dialer) streamErr(ctx context.Context, ws *websocket.Conn, id, code, message string) {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	_ = wsjson.Write(ctx, ws, protocol.Message{
		Type: protocol.MsgStreamClose,
		ID:   id,
		Error: &protocol.Error{
			Code: code, Message: message,
		},
	})
}

// dialerSink writes stream frames back through the dialer's current
// WebSocket. Serialized via writeMu.
type dialerSink struct{ d *Dialer }

func (s dialerSink) WriteReady(ctx context.Context, id string) error {
	return s.write(ctx, protocol.Message{Type: protocol.MsgStreamReady, ID: id})
}

func (s dialerSink) WriteData(ctx context.Context, id string, b []byte) error {
	enc := base64.StdEncoding.EncodeToString(b)
	payload, _ := json.Marshal(map[string]string{"b": enc})
	return s.write(ctx, protocol.Message{
		Type: protocol.MsgStreamData, ID: id, Params: payload,
	})
}

func (s dialerSink) WriteClose(ctx context.Context, id string, err error) error {
	m := protocol.Message{Type: protocol.MsgStreamClose, ID: id}
	if err != nil {
		m.Error = &protocol.Error{Code: "pty_closed", Message: err.Error()}
	}
	return s.write(ctx, m)
}

func (s dialerSink) write(ctx context.Context, msg protocol.Message) error {
	s.d.writeMu.Lock()
	ws := s.d.ws
	s.d.writeMu.Unlock()
	if ws == nil {
		return errors.New("ws closed")
	}
	wctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return wsjson.Write(wctx, ws, msg)
}

func decodeDataPayload(p json.RawMessage) ([]byte, error) {
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
