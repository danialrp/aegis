// SPDX-License-Identifier: AGPL-3.0-or-later

// Package config loads the controller's runtime configuration from
// environment variables. Every field has a sensible default so the
// binary starts cleanly against the docker-compose dev stack with
// no env vars set.
package config

import (
	"errors"
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

// MinJWTSecretLen is the minimum acceptable JWT secret length in
// bytes. 32 chars is ~256 bits of attacker-search space for HS256.
const MinJWTSecretLen = 32

// Config is the controller's full runtime configuration.
type Config struct {
	HTTPAddr        string        `env:"AEGIS_HTTP_ADDR"        envDefault:":8080"`
	DatabaseURL     string        `env:"AEGIS_DATABASE_URL"     envDefault:"postgres://aegis:aegis@localhost:5432/aegis?sslmode=disable"`
	RedisURL        string        `env:"AEGIS_REDIS_URL"        envDefault:"redis://localhost:6379/0"`
	LogLevel        string        `env:"AEGIS_LOG_LEVEL"        envDefault:"info"`
	LogFormat       string        `env:"AEGIS_LOG_FORMAT"       envDefault:"json"`
	ShutdownTimeout time.Duration `env:"AEGIS_SHUTDOWN_TIMEOUT" envDefault:"30s"`
	SkipMigrations  bool          `env:"AEGIS_SKIP_MIGRATIONS"  envDefault:"false"`

	// Auth — no default for JWTSecret on purpose: an empty secret must
	// fail startup loudly so it never reaches a real environment.
	JWTSecret       string        `env:"AEGIS_JWT_SECRET"        envDefault:""`
	AccessTokenTTL  time.Duration `env:"AEGIS_ACCESS_TOKEN_TTL"  envDefault:"15m"`
	RefreshTokenTTL time.Duration `env:"AEGIS_REFRESH_TOKEN_TTL" envDefault:"720h"` // 30 days

	// First-boot god-user bootstrap. Both must be set or both empty.
	// When set and no god user exists, one is created at startup.
	BootstrapEmail    string `env:"AEGIS_BOOTSTRAP_EMAIL"    envDefault:""`
	BootstrapPassword string `env:"AEGIS_BOOTSTRAP_PASSWORD" envDefault:""`

	// Agent endpoint (mTLS + WSS) — distinct port from the browser /
	// API endpoint so each TCP listener can run its own TLS config.
	AgentAddr       string `env:"AEGIS_AGENT_ADDR"        envDefault:":8443"`
	CADir           string `env:"AEGIS_CA_DIR"            envDefault:"/var/lib/aegis/ca"`
	AgentServerName string `env:"AEGIS_AGENT_SERVER_NAME" envDefault:"localhost"`

	// Server provisioning.
	ProvisionTimeout       time.Duration `env:"AEGIS_PROVISION_TIMEOUT"    envDefault:"10m"`
	AgentPublicURLOverride string        `env:"AEGIS_AGENT_PUBLIC_URL"     envDefault:""`

	// Per-deploy wall-clock cap. Forge's hard 10m is the bar we're
	// clearing here.
	DeployTimeout time.Duration `env:"AEGIS_DEPLOY_TIMEOUT" envDefault:"30m"`

	// SSL — email address used as the Let's Encrypt account contact.
	// Required when an operator clicks "Enable HTTPS" on a site.
	LetsEncryptEmail string `env:"AEGIS_LETSENCRYPT_EMAIL" envDefault:""`
}

// AgentPublicURL returns the WSS URL that provisioned agents will dial.
// Operator can override via AEGIS_AGENT_PUBLIC_URL; otherwise it's
// derived from AgentServerName + AgentAddr's port.
func (c Config) AgentPublicURL() string {
	if c.AgentPublicURLOverride != "" {
		return c.AgentPublicURLOverride
	}
	port := c.AgentAddr
	if len(port) > 0 && port[0] == ':' {
		port = port[1:]
	}
	return fmt.Sprintf("wss://%s:%s/agent/v1/ws", c.AgentServerName, port)
}

// Load reads configuration from the environment, applying defaults
// for any unset variables. Returns an error only for malformed
// values (e.g. an unparseable duration).
func Load() (Config, error) {
	var c Config
	if err := env.Parse(&c); err != nil {
		return Config{}, fmt.Errorf("parse env: %w", err)
	}
	return c, nil
}

// Validate reports configuration errors that must block startup.
// Anything checkable without external dependencies belongs here.
func (c Config) Validate() error {
	if c.JWTSecret == "" {
		return errors.New("AEGIS_JWT_SECRET is required (no default by design)")
	}
	if len(c.JWTSecret) < MinJWTSecretLen {
		return fmt.Errorf("AEGIS_JWT_SECRET must be at least %d characters", MinJWTSecretLen)
	}
	if (c.BootstrapEmail == "") != (c.BootstrapPassword == "") {
		return errors.New("AEGIS_BOOTSTRAP_EMAIL and AEGIS_BOOTSTRAP_PASSWORD must be set together or both empty")
	}
	return nil
}
