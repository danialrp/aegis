// SPDX-License-Identifier: AGPL-3.0-or-later

// Package dialer is the agent's outbound mTLS+WSS connection
// machinery. Run loops on the lifetime of the agent process,
// reconnecting with bounded exponential backoff whenever the WS
// drops.
package dialer

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

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
	logger    *slog.Logger
}

// New builds a Dialer.
func New(url string, tlsConfig *tls.Config, handler *rpc.Handler, logger *slog.Logger) *Dialer {
	return &Dialer{
		url:       url,
		tlsConfig: tlsConfig,
		handler:   handler,
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

	d.logger.Info("connected to controller", "url", d.url)

	for {
		var msg protocol.Message
		if err := wsjson.Read(ctx, ws, &msg); err != nil {
			return fmt.Errorf("ws read: %w", err)
		}

		switch msg.Type {
		case protocol.MsgRequest:
			d.handleRequest(ctx, ws, &msg)
		case protocol.MsgResponse:
			// Agent does not initiate RPCs in 0.7 — any response is
			// orphaned. Log and continue.
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
