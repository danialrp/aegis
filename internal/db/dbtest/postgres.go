// SPDX-License-Identifier: AGPL-3.0-or-later

// Package dbtest provides test helpers that boot a real Postgres
// instance in a Docker container, run all migrations against it, and
// hand back a connected pgx pool.
//
// The harness is intended for use under the `integration` build tag
// only — unit tests that import this package will silently skip when
// the tag is absent.
package dbtest

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver, needed by goose
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/danialrp/aegis/internal/db/migrations"
)

const (
	postgresImage = "postgres:16-alpine"
	dbName        = "aegis"
	dbUser        = "aegis"
	dbPassword    = "aegis"
)

// NewPostgres boots a fresh Postgres container, applies all migrations,
// and returns a pgxpool connected to it. The container and pool are
// cleaned up automatically via t.Cleanup.
func NewPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	container, err := tcpostgres.Run(ctx, postgresImage,
		tcpostgres.WithDatabase(dbName),
		tcpostgres.WithUsername(dbUser),
		tcpostgres.WithPassword(dbPassword),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err, "start postgres container")
	t.Cleanup(func() {
		// Terminate uses its own context so cleanup runs even after t deadline.
		_ = container.Terminate(context.Background())
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err, "obtain connection string")

	runMigrations(t, dsn)

	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err, "open pgx pool")
	t.Cleanup(pool.Close)

	require.NoError(t, pool.Ping(ctx), "ping pool")

	return pool
}

func runMigrations(t *testing.T, dsn string) {
	t.Helper()

	sqlDB, err := sql.Open("pgx", dsn)
	require.NoError(t, err, "open database/sql for migrations")
	defer func() { _ = sqlDB.Close() }()

	// Use goose's instance-based provider rather than the package-level
	// SetBaseFS/SetDialect API — the latter mutates globals and races
	// when multiple tests call NewPostgres in parallel.
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	require.NoError(t, err, "build goose provider")

	_, err = provider.Up(context.Background())
	require.NoError(t, err, "run goose migrations")
}
