// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/danialrp/aegis/pkg/protocol"
)

// HandleSiteRunScript handles host.site_run_script.
//
// Stages the script + env vars to root-owned tempfiles, invokes the
// sudoers-listed helper that drops to site_<id>, captures the merged
// stdout+stderr stream, returns it alongside the exit code.
//
// Phase 1.5 ships polling-based log delivery: the worker stores the
// returned output in the deploys row, the UI polls. A real-time
// streaming variant can replace this without changing the API
// surface visible to the browser.
func (m *Manager) HandleSiteRunScript(ctx context.Context, params json.RawMessage) (any, error) {
	var p protocol.RunScriptParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if p.SiteID < 1 || p.SiteID > 1_000_000 {
		return nil, errors.New("site_id out of range")
	}
	if p.ScriptBody == "" {
		return nil, errors.New("script_body required")
	}

	scriptPath, envPath, cleanup, err := m.stageDeploy(p)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	//nolint:gosec // helperPath is constant; siteID range-checked;
	// scriptPath/envPath are paths to files we just wrote ourselves.
	cmd := exec.CommandContext(ctx, "sudo", "-n", "--",
		helperDir+"/site_run_script",
		strconv.FormatInt(p.SiteID, 10),
		scriptPath,
		envPath,
	)
	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined

	runErr := cmd.Run()
	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("run helper: %w", runErr)
		}
	}

	return protocol.RunScriptResult{
		ExitCode: exitCode,
		Output:   combined.String(),
	}, nil
}

// stageDeploy writes the script + env file to /tmp owned by aegis
// (the agent user) — root can read them via the helper. Returns the
// paths + a cleanup func.
func (m *Manager) stageDeploy(p protocol.RunScriptParams) (scriptPath, envPath string, cleanup func(), err error) {
	scriptF, err := os.CreateTemp("", "aegis-deploy-*.sh")
	if err != nil {
		return "", "", nil, fmt.Errorf("temp script: %w", err)
	}
	if _, err := scriptF.WriteString(p.ScriptBody); err != nil {
		_ = scriptF.Close()
		_ = os.Remove(scriptF.Name())
		return "", "", nil, fmt.Errorf("write script: %w", err)
	}
	if err := scriptF.Close(); err != nil {
		_ = os.Remove(scriptF.Name())
		return "", "", nil, fmt.Errorf("close script: %w", err)
	}
	// 0644: readable by root (which the helper runs as) and by the
	// site_<id> user (which the helper drops to). Not secret.
	if err := os.Chmod(scriptF.Name(), 0o644); err != nil { //nolint:gosec // intentional: see comment
		_ = os.Remove(scriptF.Name())
		return "", "", nil, fmt.Errorf("chmod script: %w", err)
	}

	envF, err := os.CreateTemp("", "aegis-deploy-env-*")
	if err != nil {
		_ = os.Remove(scriptF.Name())
		return "", "", nil, fmt.Errorf("temp env: %w", err)
	}
	for k, v := range p.EnvVars {
		if !validEnvKey(k) {
			_ = envF.Close()
			_ = os.Remove(envF.Name())
			_ = os.Remove(scriptF.Name())
			return "", "", nil, fmt.Errorf("invalid env key: %q", k)
		}
		if _, err := fmt.Fprintf(envF, "%s=%s\n", k, shellQuoteForEnv(v)); err != nil {
			_ = envF.Close()
			_ = os.Remove(envF.Name())
			_ = os.Remove(scriptF.Name())
			return "", "", nil, fmt.Errorf("write env: %w", err)
		}
	}
	if err := envF.Close(); err != nil {
		_ = os.Remove(envF.Name())
		_ = os.Remove(scriptF.Name())
		return "", "", nil, fmt.Errorf("close env: %w", err)
	}

	cleanup = func() {
		_ = os.Remove(scriptF.Name())
		_ = os.Remove(envF.Name())
	}
	return scriptF.Name(), envF.Name(), cleanup, nil
}

func validEnvKey(k string) bool {
	if k == "" {
		return false
	}
	for i, r := range k {
		switch {
		case r == '_':
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

func shellQuoteForEnv(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'"'"'`) + "'"
}
