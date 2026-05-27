// SPDX-License-Identifier: AGPL-3.0-or-later

// Package rpc holds the agent-side dispatch table for RPCs received
// from the controller. In 0.7 only `ping` is registered; host
// primitives (Linux user mgmt, nginx, docker) plug in here in
// Phase 1+.
package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/danialrp/aegis/pkg/protocol"
)

// HandlerFunc is the signature every method handler implements.
// params is left as raw JSON so each handler unmarshals into its own
// typed params struct.
type HandlerFunc func(ctx context.Context, params json.RawMessage) (any, error)

// Handler is the agent's RPC router. Use Register to add methods,
// Dispatch from the WS read loop.
type Handler struct {
	logger *slog.Logger

	mu       sync.RWMutex
	handlers map[string]HandlerFunc
}

// New builds a Handler with the always-on methods registered.
func New(logger *slog.Logger) *Handler {
	h := &Handler{
		logger:   logger,
		handlers: make(map[string]HandlerFunc),
	}
	h.Register(protocol.MethodPing, h.handlePing)
	return h
}

// Register adds (or replaces) the handler for method.
func (h *Handler) Register(method string, fn HandlerFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handlers[method] = fn
}

// Dispatch looks up method and calls its handler. Returns
// (nil, *protocol.Error) instead of a Go error so the caller can
// directly populate a Response.
func (h *Handler) Dispatch(ctx context.Context, method string, params json.RawMessage) (any, *protocol.Error) {
	h.mu.RLock()
	fn, ok := h.handlers[method]
	h.mu.RUnlock()
	if !ok {
		return nil, &protocol.Error{
			Code:    protocol.ErrCodeUnknownMethod,
			Message: fmt.Sprintf("unknown method: %s", method),
		}
	}

	result, err := fn(ctx, params)
	if err != nil {
		return nil, &protocol.Error{
			Code:    protocol.ErrCodeInternal,
			Message: err.Error(),
		}
	}
	return result, nil
}

func (h *Handler) handlePing(_ context.Context, params json.RawMessage) (any, error) {
	var p protocol.PingParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("decode ping params: %w", err)
		}
	}
	return protocol.PongResult{
		SentAt: p.SentAt,
		PongAt: time.Now().UTC(),
	}, nil
}
