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
	"github.com/danialrp/aegis/internal/agent/host"
	agentpty "github.com/danialrp/aegis/internal/agent/pty"
	"github.com/danialrp/aegis/internal/agent/rpc"
	"github.com/danialrp/aegis/internal/logging"
	"github.com/danialrp/aegis/internal/version"
	"github.com/danialrp/aegis/pkg/protocol"
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

	// Host-management RPCs (Phase 1.2+).
	hostMgr := host.New(logger)
	handler.Register(protocol.MethodHostSiteUserCreate, hostMgr.HandleSiteUserCreate)
	handler.Register(protocol.MethodHostSiteDirEnsure, hostMgr.HandleSiteDirEnsure)
	handler.Register(protocol.MethodHostSiteDelete, hostMgr.HandleSiteDelete)
	handler.Register(protocol.MethodHostNginxApplyVhost, hostMgr.HandleNginxApplyVhost)
	handler.Register(protocol.MethodHostNginxRemoveVhost, hostMgr.HandleNginxRemoveVhost)
	handler.Register(protocol.MethodHostSiteRunScript, hostMgr.HandleSiteRunScript)
	handler.Register(protocol.MethodHostCertIssue, hostMgr.HandleCertIssue)
	handler.Register(protocol.MethodHostCertRemove, hostMgr.HandleCertRemove)
	handler.Register(protocol.MethodHostCertStatus, hostMgr.HandleCertStatus)
	handler.Register(protocol.MethodHostDaemonWrite, hostMgr.HandleDaemonWrite)
	handler.Register(protocol.MethodHostDaemonRemove, hostMgr.HandleDaemonRemove)
	handler.Register(protocol.MethodHostDaemonAction, hostMgr.HandleDaemonAction)
	handler.Register(protocol.MethodHostDaemonLogs, hostMgr.HandleDaemonLogs)
	handler.Register(protocol.MethodHostNginxApplyProxyVhost, hostMgr.HandleNginxApplyProxyVhost)
	handler.Register(protocol.MethodHostComposeWrite, hostMgr.HandleComposeWrite)
	handler.Register(protocol.MethodHostComposeAction, hostMgr.HandleComposeAction)
	handler.Register(protocol.MethodHostComposePs, hostMgr.HandleComposePs)
	handler.Register(protocol.MethodHostComposeLogs, hostMgr.HandleComposeLogs)
	handler.Register(protocol.MethodHostPhpFpmPoolWrite, hostMgr.HandlePhpFpmPoolWrite)
	handler.Register(protocol.MethodHostPhpFpmPoolRemove, hostMgr.HandlePhpFpmPoolRemove)
	handler.Register(protocol.MethodHostNginxApplyPhpVhost, hostMgr.HandleNginxApplyPhpVhost)
	handler.Register(protocol.MethodHostMysqlDBCreate, hostMgr.HandleDBCreate)
	handler.Register(protocol.MethodHostMysqlDBDrop, hostMgr.HandleDBDrop)
	handler.Register(protocol.MethodHostPostgresDBCreate, hostMgr.HandleDBCreate)
	handler.Register(protocol.MethodHostPostgresDBDrop, hostMgr.HandleDBDrop)
	handler.Register(protocol.MethodHostDBBackup, hostMgr.HandleDBBackup)
	handler.Register(protocol.MethodHostDBRestore, hostMgr.HandleDBRestore)
	handler.Register(protocol.MethodHostDBBackupsList, hostMgr.HandleDBBackupsList)
	handler.Register(protocol.MethodHostMetrics, hostMgr.HandleMetrics)

	ptyMgr := agentpty.New(logger)
	d := dialer.New(cfg.ControllerURL, tlsCfg, handler, ptyMgr, logger)

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
