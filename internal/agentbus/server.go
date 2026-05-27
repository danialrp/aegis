// SPDX-License-Identifier: AGPL-3.0-or-later

package agentbus

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/danialrp/aegis/internal/ca"
)

// ServerOptions configures the mTLS+WSS endpoint that agents dial.
type ServerOptions struct {
	Addr           string // e.g. ":8443"
	ControllerName string // CN/SAN for the controller's own server cert (e.g. "controller.example.com")
	ExtraDNSNames  []string
	ExtraIPs       []net.IP
}

// NewServer builds an *http.Server pre-wired with:
//
//   - tls.Config requiring + verifying client certs against the
//     internal CA
//   - a freshly-issued server cert signed by the same CA
//   - one route: GET /agent/v1/ws → Hub.Handler()
//
// The caller is responsible for calling Serve and Shutdown.
func NewServer(opts ServerOptions, c *ca.CA, hub *Hub) (*http.Server, error) {
	dnsNames := append([]string{opts.ControllerName, "localhost"}, opts.ExtraDNSNames...)
	ips := append([]net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}, opts.ExtraIPs...)

	serverCert, err := c.IssueServerCert(opts.ControllerName, dedupe(dnsNames), dedupeIPs(ips))
	if err != nil {
		return nil, fmt.Errorf("issue controller server cert: %w", err)
	}

	keypair, err := tls.X509KeyPair(serverCert.CertPEM, serverCert.KeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse server keypair: %w", err)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{keypair},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    c.CertPool(),
		MinVersion:   tls.VersionTLS13,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /agent/v1/ws", hub.Handler())

	return &http.Server{
		Addr:              opts.Addr,
		Handler:           mux,
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}, nil
}

// ServeTLS starts srv on a TLS listener built from srv.TLSConfig.
// Blocks until the listener stops; returns nil on graceful shutdown.
func ServeTLS(ctx context.Context, srv *http.Server) error {
	ln, err := tls.Listen("tcp", srv.Addr, srv.TLSConfig)
	if err != nil {
		return fmt.Errorf("listen %s: %w", srv.Addr, err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		// Use a fresh background context for Shutdown: the parent ctx
		// is the very thing that just fired, so deriving from it would
		// return an already-cancelled context and force a hard close.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		//nolint:contextcheck // see comment above — fresh deadline is intentional
		_ = srv.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func dedupe(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func dedupeIPs(in []net.IP) []net.IP {
	seen := make(map[string]struct{}, len(in))
	out := make([]net.IP, 0, len(in))
	for _, ip := range in {
		if ip == nil {
			continue
		}
		key := ip.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ip)
	}
	return out
}
