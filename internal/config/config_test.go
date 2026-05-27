// SPDX-License-Identifier: AGPL-3.0-or-later

package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/danialrp/aegis/internal/config"
)

func TestLoadDefaults(t *testing.T) {
	// t.Setenv unsets after test; ensures defaults are exercised even if
	// the developer's shell happens to export AEGIS_* vars.
	for _, k := range []string{
		"AEGIS_HTTP_ADDR", "AEGIS_DATABASE_URL", "AEGIS_REDIS_URL",
		"AEGIS_LOG_LEVEL", "AEGIS_LOG_FORMAT",
		"AEGIS_SHUTDOWN_TIMEOUT", "AEGIS_SKIP_MIGRATIONS",
	} {
		t.Setenv(k, "")
	}

	cfg, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, ":8080", cfg.HTTPAddr)
	require.Equal(t, "postgres://aegis:aegis@localhost:5432/aegis?sslmode=disable", cfg.DatabaseURL)
	require.Equal(t, "redis://localhost:6379/0", cfg.RedisURL)
	require.Equal(t, "info", cfg.LogLevel)
	require.Equal(t, "json", cfg.LogFormat)
	require.Equal(t, 30*time.Second, cfg.ShutdownTimeout)
	require.False(t, cfg.SkipMigrations)
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("AEGIS_HTTP_ADDR", "127.0.0.1:9090")
	t.Setenv("AEGIS_LOG_LEVEL", "debug")
	t.Setenv("AEGIS_LOG_FORMAT", "text")
	t.Setenv("AEGIS_SHUTDOWN_TIMEOUT", "5s")
	t.Setenv("AEGIS_SKIP_MIGRATIONS", "true")

	cfg, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:9090", cfg.HTTPAddr)
	require.Equal(t, "debug", cfg.LogLevel)
	require.Equal(t, "text", cfg.LogFormat)
	require.Equal(t, 5*time.Second, cfg.ShutdownTimeout)
	require.True(t, cfg.SkipMigrations)
}

func TestLoadMalformedDuration(t *testing.T) {
	t.Setenv("AEGIS_SHUTDOWN_TIMEOUT", "not-a-duration")
	_, err := config.Load()
	require.Error(t, err)
}

func TestValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		mutate  func(*config.Config)
		wantErr string
	}{
		{
			name:    "empty JWT secret",
			mutate:  func(c *config.Config) { c.JWTSecret = "" },
			wantErr: "AEGIS_JWT_SECRET is required",
		},
		{
			name:    "short JWT secret",
			mutate:  func(c *config.Config) { c.JWTSecret = "too-short" },
			wantErr: "at least 32",
		},
		{
			name: "bootstrap email without password",
			mutate: func(c *config.Config) {
				c.BootstrapEmail = "x@y.z"
				c.BootstrapPassword = ""
			},
			wantErr: "must be set together",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Config{JWTSecret: "x-thirty-two-chars-long-secret-x"}
			tc.mutate(&cfg)
			err := cfg.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}

	// Valid case
	cfg := config.Config{JWTSecret: "x-thirty-two-chars-long-secret-x"}
	require.NoError(t, cfg.Validate())
}
