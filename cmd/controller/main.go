// SPDX-License-Identifier: AGPL-3.0-or-later

// Command aegis-controller is the Aegis control-plane binary.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/riverqueue/river"

	"github.com/danialrp/aegis/internal/agentbinary"
	"github.com/danialrp/aegis/internal/agentbus"
	"github.com/danialrp/aegis/internal/api"
	"github.com/danialrp/aegis/internal/audit"
	"github.com/danialrp/aegis/internal/auth"
	"github.com/danialrp/aegis/internal/ca"
	"github.com/danialrp/aegis/internal/config"
	"github.com/danialrp/aegis/internal/db"
	"github.com/danialrp/aegis/internal/db/sqlc"
	"github.com/danialrp/aegis/internal/jobs"
	"github.com/danialrp/aegis/internal/logging"
	"github.com/danialrp/aegis/internal/provisioner"
	"github.com/danialrp/aegis/internal/version"
	"github.com/danialrp/aegis/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("controller exited with error", "err", err)
		os.Exit(1)
	}
}

//nolint:funlen // wiring main is intentionally long and linear
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	logger := logging.New(os.Stdout, cfg.LogLevel, cfg.LogFormat)
	slog.SetDefault(logger)

	logger.Info("aegis-controller starting",
		"version", version.Version,
		"commit", version.Commit,
		"built", version.Date,
		"http_addr", cfg.HTTPAddr,
		"agent_addr", cfg.AgentAddr,
		"agent_binary_embedded", agentbinary.Embedded(),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer pool.Close()
	logger.Info("postgres connected")

	if cfg.SkipMigrations {
		logger.Warn("skipping migrations (AEGIS_SKIP_MIGRATIONS=true)")
	} else {
		if err := db.Migrate(ctx, cfg.DatabaseURL); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		if err := jobs.Migrate(ctx, pool); err != nil {
			return fmt.Errorf("river migrate: %w", err)
		}
		logger.Info("migrations applied")
	}

	queries := sqlc.New(pool)
	auditRec := audit.New(queries)
	authSvc := auth.NewService(pool, auditRec, []byte(cfg.JWTSecret), cfg.AccessTokenTTL, cfg.RefreshTokenTTL)

	if err := authSvc.BootstrapGodIfMissing(ctx, cfg.BootstrapEmail, cfg.BootstrapPassword); err != nil {
		return fmt.Errorf("bootstrap god: %w", err)
	}
	if cfg.BootstrapEmail != "" {
		logger.Info("bootstrap god user processed", "email", cfg.BootstrapEmail)
	}

	internalCA, err := ca.OpenOrCreate(cfg.CADir)
	if err != nil {
		return fmt.Errorf("open ca: %w", err)
	}
	logger.Info("internal CA ready", "dir", cfg.CADir)

	hub := agentbus.NewHub(logger, queries)

	agentSrv, err := agentbus.NewServer(agentbus.ServerOptions{
		Addr:           cfg.AgentAddr,
		ControllerName: cfg.AgentServerName,
	}, internalCA, hub)
	if err != nil {
		return fmt.Errorf("build agent server: %w", err)
	}

	// river setup
	workers := river.NewWorkers()
	river.AddWorker(workers, &jobs.ProvisionWorker{
		Queries:       queries,
		Audit:         auditRec,
		CA:            internalCA,
		Provisioner:   provisioner.NewSSH(),
		AgentBinary:   agentbinary.For,
		ControllerURL: cfg.AgentPublicURL(),
		WaitTimeout:   cfg.ProvisionTimeout,
		Logger:        logger,
	})
	rt, err := jobs.Setup(pool, logger, workers)
	if err != nil {
		return fmt.Errorf("setup river: %w", err)
	}
	if err := rt.Start(ctx); err != nil {
		return fmt.Errorf("start river: %w", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = rt.Stop(stopCtx)
	}()

	spaFS, err := web.FS()
	if err != nil {
		return fmt.Errorf("open embedded spa fs: %w", err)
	}

	handler := api.NewRouter(api.Deps{
		Logger:      logger,
		Pool:        pool,
		AuthService: authSvc,
		JWTSecret:   []byte(cfg.JWTSecret),
		Queries:     queries,
		Audit:       auditRec,
		RiverClient: rt.Client(),
		SPA:         spaFS,
	})

	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 2)
	go func() {
		logger.Info("http listening", "addr", cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server: %w", err)
		}
	}()
	go func() {
		logger.Info("agent endpoint listening (mTLS)", "addr", cfg.AgentAddr)
		if err := agentbus.ServeTLS(ctx, agentSrv); err != nil &&
			!errors.Is(err, context.Canceled) &&
			!errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("agent server: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		stop()
		logger.Error("server error, draining peer", "err", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("http shutdown error", "err", err)
	}
	logger.Info("controller stopped cleanly")
	return nil
}
