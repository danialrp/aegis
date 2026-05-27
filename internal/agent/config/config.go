// SPDX-License-Identifier: AGPL-3.0-or-later

// Package config loads the agent's runtime configuration. Distinct
// from the controller's config package because the field set, env
// prefix, and defaults differ.
package config

import (
	"errors"
	"fmt"

	"github.com/caarlos0/env/v11"
)

// Config is the agent's runtime configuration.
type Config struct {
	ControllerURL string `env:"AEGIS_CONTROLLER_URL,required"`
	ControllerCA  string `env:"AEGIS_CONTROLLER_CA,required"`
	AgentCert     string `env:"AEGIS_AGENT_CERT,required"`
	AgentKey      string `env:"AEGIS_AGENT_KEY,required"`

	LogLevel  string `env:"AEGIS_LOG_LEVEL"  envDefault:"info"`
	LogFormat string `env:"AEGIS_LOG_FORMAT" envDefault:"json"`
}

// Load reads env vars. Returns an error if any required value is
// unset — there are no real defaults for the cert paths and URL.
func Load() (Config, error) {
	var c Config
	if err := env.Parse(&c); err != nil {
		return Config{}, fmt.Errorf("parse env: %w", err)
	}
	if c.ControllerURL == "" {
		return Config{}, errors.New("AEGIS_CONTROLLER_URL is required")
	}
	return c, nil
}
