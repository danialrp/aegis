// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build integration

package agentbus_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/danialrp/aegis/internal/agent/dialer"
	"github.com/danialrp/aegis/internal/agent/rpc"
	"github.com/danialrp/aegis/internal/agentbus"
	"github.com/danialrp/aegis/internal/ca"
	"github.com/danialrp/aegis/pkg/protocol"
)

// TestEchoRoundTrip is the headline 0.7 verification: a controller and
// an agent run in the same process, the agent dials in over mTLS+WSS,
// and a ping/pong RPC completes end-to-end.
func TestEchoRoundTrip(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// 1. Internal CA.
	c, err := ca.OpenOrCreate(t.TempDir())
	require.NoError(t, err)

	// 2. Controller-side server cert (used by the TLS listener).
	hub := agentbus.NewHub(logger, nil)
	srv, err := agentbus.NewServer(agentbus.ServerOptions{
		// :0 → kernel-assigned ephemeral port; we read it back below.
		Addr:           "127.0.0.1:0",
		ControllerName: "localhost",
	}, c, hub)
	require.NoError(t, err)

	ln, err := tls.Listen("tcp", "127.0.0.1:0", srv.TLSConfig)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	addr := ln.Addr().(*net.TCPAddr)

	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	})

	// 3. Agent-side: cert for server_id=42, written to a tmp dir so
	//    the dialer's existing file-loading path is exercised.
	const serverID int64 = 42
	bundle, err := c.IssueAgentCert(serverID)
	require.NoError(t, err)

	certDir := t.TempDir()
	certPath := filepath.Join(certDir, "agent.crt")
	keyPath := filepath.Join(certDir, "agent.key")
	caPath := filepath.Join(certDir, "controller-ca.crt")
	require.NoError(t, os.WriteFile(certPath, bundle.CertPEM, 0o600))
	require.NoError(t, os.WriteFile(keyPath, bundle.KeyPEM, 0o600))
	require.NoError(t, os.WriteFile(caPath, c.CACertPEM(), 0o600))

	// 4. Build the agent dialer in-process.
	tlsCert, err := tls.LoadX509KeyPair(certPath, keyPath)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(c.CACertPEM())

	wsURL := (&url.URL{
		Scheme: "wss",
		Host:   fmt.Sprintf("localhost:%d", addr.Port),
		Path:   "/agent/v1/ws",
	}).String()

	handler := rpc.New(logger)
	d := dialer.New(wsURL, &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		RootCAs:      pool,
		MinVersion:   tls.VersionTLS13,
	}, handler, logger)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = d.Run(ctx)
	}()
	// Cleanup runs LIFO. Register wg.Wait FIRST so it runs LAST;
	// cancel runs first, unblocks the goroutine, then wg.Wait returns.
	t.Cleanup(wg.Wait)
	t.Cleanup(cancel)

	// 5. Wait until the hub sees the connection. Generous deadline —
	//    container startup can be slow on busy CI hosts.
	deadline := time.Now().Add(5 * time.Second)
	var conn *agentbus.Conn
	for time.Now().Before(deadline) {
		if c, ok := hub.Get(serverID); ok {
			conn = c
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.NotNil(t, conn, "agent never registered with hub")

	// 6. The actual round-trip.
	reqCtx, reqCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer reqCancel()

	sentAt := time.Now().UTC().Truncate(time.Millisecond)
	resp, err := conn.Request(reqCtx, protocol.MethodPing, protocol.PingParams{SentAt: sentAt})
	require.NoError(t, err)
	require.Nil(t, resp.Error)

	var pong protocol.PongResult
	require.NoError(t, json.Unmarshal(resp.Result, &pong))
	require.True(t, pong.SentAt.Equal(sentAt), "agent must echo sent_at")
	require.False(t, pong.PongAt.IsZero(), "agent must populate pong_at")
}

// TestUnknownMethodReturnsError verifies the dialer's dispatch error
// path: an RPC the agent has not registered comes back as a typed
// protocol.Error rather than hanging.
func TestUnknownMethodReturnsError(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	c, err := ca.OpenOrCreate(t.TempDir())
	require.NoError(t, err)

	hub := agentbus.NewHub(logger, nil)
	srv, err := agentbus.NewServer(agentbus.ServerOptions{
		Addr:           "127.0.0.1:0",
		ControllerName: "localhost",
	}, c, hub)
	require.NoError(t, err)

	ln, err := tls.Listen("tcp", "127.0.0.1:0", srv.TLSConfig)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	addr := ln.Addr().(*net.TCPAddr)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		sctx, c := context.WithTimeout(context.Background(), 2*time.Second)
		defer c()
		_ = srv.Shutdown(sctx)
	})

	const serverID int64 = 7
	bundle, _ := c.IssueAgentCert(serverID)
	tlsCert, err := tls.X509KeyPair(bundle.CertPEM, bundle.KeyPEM)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(c.CACertPEM())

	wsURL := fmt.Sprintf("wss://localhost:%d/agent/v1/ws", addr.Port)

	d := dialer.New(wsURL, &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		RootCAs:      pool,
		MinVersion:   tls.VersionTLS13,
	}, rpc.New(logger), logger)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = d.Run(ctx) }()
	t.Cleanup(wg.Wait)
	t.Cleanup(cancel)

	deadline := time.Now().Add(5 * time.Second)
	var conn *agentbus.Conn
	for time.Now().Before(deadline) {
		if c, ok := hub.Get(serverID); ok {
			conn = c
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.NotNil(t, conn)

	reqCtx, reqCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer reqCancel()
	resp, err := conn.Request(reqCtx, "nonexistent.method", nil)
	require.Error(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	require.Equal(t, protocol.ErrCodeUnknownMethod, resp.Error.Code)
}
