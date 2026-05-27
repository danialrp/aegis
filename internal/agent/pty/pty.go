// SPDX-License-Identifier: AGPL-3.0-or-later

// Package pty owns the agent-side PTY lifecycle for the web-terminal
// flow. Stream-open from the controller spawns `su - site_<id>`
// against a fresh PTY; bytes flow bidirectionally over the agent WS
// stream protocol.
package pty

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strconv"
	"sync"

	"github.com/creack/pty"

	"github.com/danialrp/aegis/pkg/protocol"
)

// Manager spawns + tracks PTYs per stream ID. One PTY per stream.
type Manager struct {
	logger *slog.Logger

	mu      sync.Mutex
	streams map[string]*session
}

// New builds an empty Manager.
func New(logger *slog.Logger) *Manager {
	return &Manager{
		logger:  logger,
		streams: make(map[string]*session),
	}
}

// session is one active PTY/stream pair.
type session struct {
	cmd  *exec.Cmd
	tty  io.ReadWriteCloser
	stop func()
}

// Sink is what Manager writes outbound bytes to. The dialer
// implements this by encoding chunks as stream_data WS frames.
type Sink interface {
	WriteData(ctx context.Context, streamID string, b []byte) error
	WriteClose(ctx context.Context, streamID string, err error) error
	WriteReady(ctx context.Context, streamID string) error
}

// Open handles a stream_open with method=host.pty_open.
//
// Spawns a PTY, sends stream_ready, and launches a goroutine that
// reads from the PTY and forwards each chunk to sink as stream_data
// until the process exits or Close is called.
func (m *Manager) Open(ctx context.Context, streamID string, params json.RawMessage, sink Sink) error {
	var p protocol.PtyOpenParams
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("decode params: %w", err)
	}
	if p.SiteID < 1 || p.SiteID > 1_000_000 {
		return errors.New("site_id out of range")
	}

	user := "site_" + strconv.FormatInt(p.SiteID, 10)

	// Session lifetime is decoupled from the open-RPC's ctx — the
	// connection-level ctx (e.g. agent shutdown) is what kills the
	// shell. Detached from the inbound ctx by design.
	sessCtx, cancel := context.WithCancel(context.Background()) //nolint:contextcheck // detached on purpose; see comment
	//nolint:gosec,contextcheck // user is range-checked; ctx detached by design
	cmd := exec.CommandContext(sessCtx, "sudo", "-n", "-u", user, "-i", "--", "/bin/bash", "-l")
	cmd.Env = []string{
		"TERM=xterm-256color",
		"LANG=C.UTF-8",
		"HOME=/srv/sites/" + strconv.FormatInt(p.SiteID, 10),
	}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		cancel()
		return fmt.Errorf("pty start: %w", err)
	}
	if p.Cols > 0 && p.Rows > 0 {
		_ = pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(p.Cols), Rows: uint16(p.Rows)}) //nolint:gosec // bounded
	}

	sess := &session{cmd: cmd, tty: ptmx, stop: cancel}

	m.mu.Lock()
	m.streams[streamID] = sess
	m.mu.Unlock()

	if err := sink.WriteReady(ctx, streamID); err != nil {
		m.tearDown(streamID)
		return fmt.Errorf("send ready: %w", err)
	}

	//nolint:contextcheck // sessCtx is the session's own lifetime, intentionally detached
	go m.pump(sessCtx, streamID, ptmx, sink)
	return nil
}

// Write forwards a chunk received from the controller into the PTY.
func (m *Manager) Write(streamID string, b []byte) error {
	m.mu.Lock()
	s, ok := m.streams[streamID]
	m.mu.Unlock()
	if !ok {
		return errors.New("unknown stream")
	}
	_, err := s.tty.Write(b)
	return err
}

// Close tears down a PTY in response to a stream_close from the peer.
func (m *Manager) Close(streamID string) {
	m.tearDown(streamID)
}

func (m *Manager) tearDown(streamID string) {
	m.mu.Lock()
	s, ok := m.streams[streamID]
	delete(m.streams, streamID)
	m.mu.Unlock()
	if !ok {
		return
	}
	s.stop()
	_ = s.tty.Close()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
}

func (m *Manager) pump(ctx context.Context, streamID string, tty io.Reader, sink Sink) {
	defer m.tearDown(streamID)

	buf := make([]byte, 4096)
	for {
		if ctx.Err() != nil {
			return
		}
		n, err := tty.Read(buf)
		if n > 0 {
			if werr := sink.WriteData(ctx, streamID, buf[:n]); werr != nil {
				m.logger.Warn("pty sink write failed", "err", werr)
				return
			}
		}
		if err != nil {
			closeErr := err
			if errors.Is(err, io.EOF) {
				closeErr = nil
			}
			_ = sink.WriteClose(ctx, streamID, closeErr)
			return
		}
	}
}
