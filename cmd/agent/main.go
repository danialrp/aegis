// SPDX-License-Identifier: AGPL-3.0-or-later

// Command aegis-agent is the per-server Aegis agent binary.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/danialrp/aegis/internal/agent/config"
	"github.com/danialrp/aegis/internal/agent/dialer"
	"github.com/danialrp/aegis/internal/agent/rpc"
	"github.com/danialrp/aegis/internal/logging"
	"github.com/danialrp/aegis/internal/version"
)

func main() {
	if err := run(); err != nil {
		slog.Error("agent exited with error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := logging.New(os.Stdout, cfg.LogLevel, cfg.LogFormat)
	slog.SetDefault(logger)

	logger.Info("aegis-agent starting",
		"version", version.Version,
		"commit", version.Commit,
		"built", version.Date,
		"controller", cfg.ControllerURL,
	)

	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		return fmt.Errorf("build tls config: %w", err)
	}

	handler := rpc.New(logger)
	d := dialer.New(cfg.ControllerURL, tlsCfg, handler, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := d.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("dialer: %w", err)
	}

	logger.Info("agent stopped cleanly")
	return nil
}

func buildTLSConfig(cfg config.Config) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(cfg.AgentCert, cfg.AgentKey)
	if err != nil {
		return nil, fmt.Errorf("load agent keypair: %w", err)
	}

	caPEM, err := os.ReadFile(cfg.ControllerCA)
	if err != nil {
		return nil, fmt.Errorf("read controller ca: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("controller ca file contained no usable certs")
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		MinVersion:   tls.VersionTLS13,
	}, nil
}
