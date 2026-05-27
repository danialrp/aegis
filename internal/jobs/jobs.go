// SPDX-License-Identifier: AGPL-3.0-or-later

// Package jobs wraps river's background-job runtime: client setup,
// migrations, worker registration, and a small Start/Stop API the
// controller's main.go uses.
package jobs

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// Runtime owns the river client and the database driver. Use Start
// to bring the worker pool online and Stop for graceful shutdown.
type Runtime struct {
	client *river.Client[pgx.Tx]
	logger *slog.Logger
}

// Migrate applies river's own schema migrations to the given pool.
// Idempotent: safe on every startup.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	m, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("build river migrator: %w", err)
	}
	if _, err := m.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("river migrate up: %w", err)
	}
	return nil
}

// Setup builds the river client with all of our workers registered.
// Workers are registered via the WorkerSet passed in so callers
// retain control over their dependencies (sqlc handle, provisioner,
// CA, audit recorder, etc.).
func Setup(pool *pgxpool.Pool, logger *slog.Logger, workers *river.Workers) (*Runtime, error) {
	c, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 4},
		},
		Workers: workers,
		Logger:  logger,
	})
	if err != nil {
		return nil, fmt.Errorf("river new client: %w", err)
	}
	return &Runtime{client: c, logger: logger}, nil
}

// Client returns the underlying river client so callers can Enqueue
// jobs.
func (r *Runtime) Client() *river.Client[pgx.Tx] { return r.client }

// Start brings the worker pool online. Blocks only briefly during
// initial setup; long-running work happens in goroutines spawned by
// river itself.
func (r *Runtime) Start(ctx context.Context) error {
	if err := r.client.Start(ctx); err != nil {
		return fmt.Errorf("river start: %w", err)
	}
	r.logger.Info("river worker pool started")
	return nil
}

// Stop drains in-flight jobs up to the deadline carried by ctx, then
// shuts the workers down.
func (r *Runtime) Stop(ctx context.Context) error {
	if err := r.client.Stop(ctx); err != nil {
		return fmt.Errorf("river stop: %w", err)
	}
	r.logger.Info("river worker pool stopped")
	return nil
}
