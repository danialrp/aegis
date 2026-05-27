// SPDX-License-Identifier: AGPL-3.0-or-later

// Package db wires the controller to Postgres: opening the pgx pool
// (with bounded retry so a slow-to-start Postgres during dev/compose
// boot doesn't cause an immediate exit) and applying goose migrations
// from the embedded migration FS.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver for goose
	"github.com/pressly/goose/v3"

	"github.com/danialrp/aegis/internal/db/migrations"
)

const (
	connectAttempts    = 5
	connectPingTimeout = 3 * time.Second
)

// OpenPool parses dsn, opens a pgxpool, and pings it with bounded
// retry. Returns the connected pool or an error after all attempts
// have failed.
func OpenPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("new pool: %w", err)
	}

	var lastErr error
	for i := range connectAttempts {
		pingCtx, cancel := context.WithTimeout(ctx, connectPingTimeout)
		err := pool.Ping(pingCtx)
		cancel()
		if err == nil {
			return pool, nil
		}
		lastErr = err

		// Linear backoff: 1s, 2s, 3s, 4s, 5s (cap by ctx).
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(time.Duration(i+1) * time.Second):
		}
	}

	pool.Close()
	return nil, fmt.Errorf("db unreachable after %d attempts: %w", connectAttempts, lastErr)
}

// Migrate applies all goose migrations against dsn. Safe to call on
// every startup — goose tracks applied versions in its own table.
//
// Uses the instance-based goose.NewProvider rather than the package-
// global SetBaseFS/SetDialect helpers; the latter aren't safe for
// concurrent use and provide no real ergonomic benefit here.
func Migrate(ctx context.Context, dsn string) (err error) {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open sql: %w", err)
	}
	defer func() {
		if cerr := sqlDB.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close sql: %w", cerr)
		}
	}()

	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		return fmt.Errorf("build goose provider: %w", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}
