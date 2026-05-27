// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build integration

package v1_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"

	"github.com/danialrp/aegis/internal/api"
	"github.com/danialrp/aegis/internal/audit"
	"github.com/danialrp/aegis/internal/auth"
	"github.com/danialrp/aegis/internal/ca"
	"github.com/danialrp/aegis/internal/db/dbtest"
	"github.com/danialrp/aegis/internal/db/sqlc"
	"github.com/danialrp/aegis/internal/jobs"
	"github.com/danialrp/aegis/internal/provisioner"
)

var serversSecret = []byte("servers-integration-secret-32-x")

type serverDTO struct {
	ID               int64   `json:"id"`
	Name             string  `json:"name"`
	PublicIP         string  `json:"public_ip"`
	SSHUser          string  `json:"ssh_user"`
	Status           string  `json:"status"`
	AgentFingerprint *string `json:"agent_fingerprint,omitempty"`
	AgentLastSeen    *string `json:"agent_last_seen,omitempty"`
	ProvisionError   *string `json:"provision_error,omitempty"`
}

type serversRig struct {
	srv         *httptest.Server
	queries     *sqlc.Queries
	bearer      string
	fakeProv    *provisioner.FakeProvisioner
	riverClient *river.Client[pgx.Tx]
	logger      *slog.Logger
}

//nolint:funlen // setup wires many pieces — clarity beats brevity
func newServersRig(t *testing.T) *serversRig {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool := dbtest.NewPostgres(t)
	queries := sqlc.New(pool)
	rec := audit.New(queries)

	// river migrations
	require.NoError(t, jobs.Migrate(context.Background(), pool))

	// CA in tmpdir
	c, err := ca.OpenOrCreate(t.TempDir())
	require.NoError(t, err)

	// fake provisioner that simulates the agent dialing in by stamping
	// agent_last_seen on the server row.
	fake := provisioner.NewFake()
	fake.SetOnCall(func(ctx context.Context, target provisioner.Target) error {
		return queries.TouchServerAgent(ctx, target.ServerID)
	})

	// agent binary "fetcher" that always returns a non-empty byte slice
	// (size > the 1MB sanity check is not enforced in the test path).
	agentBinary := func(arch string) ([]byte, error) {
		return bytes.Repeat([]byte("X"), 16), nil
	}

	workers := river.NewWorkers()
	river.AddWorker(workers, &jobs.ProvisionWorker{
		Queries:       queries,
		Audit:         rec,
		CA:            c,
		Provisioner:   fake,
		AgentBinary:   agentBinary,
		ControllerURL: "wss://localhost:8443/agent/v1/ws",
		WaitTimeout:   5 * time.Second,
		Logger:        logger,
	})
	rt, err := jobs.Setup(pool, logger, workers)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, rt.Start(ctx))
	t.Cleanup(func() {
		cancel()
		stopCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = rt.Stop(stopCtx)
	})

	authSvc := auth.NewService(pool, rec, serversSecret, 15*time.Minute, 24*time.Hour)
	handler := api.NewRouter(api.Deps{
		Logger:      logger,
		Pool:        pool,
		AuthService: authSvc,
		JWTSecret:   serversSecret,
		Queries:     queries,
		Audit:       rec,
		RiverClient: rt.Client(),
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	// Seed a god user + log in to get a bearer.
	hash, err := auth.HashPassword("correct-horse-battery-staple")
	require.NoError(t, err)
	_, err = queries.CreateUser(context.Background(), sqlc.CreateUserParams{
		Email: "admin@example.com", PasswordHash: hash, Role: "god", Enabled: true,
	})
	require.NoError(t, err)

	resp := postJSON(t, srv.URL+"/v1/auth/login", map[string]string{
		"email": "admin@example.com", "password": "correct-horse-battery-staple",
	}, "")
	var tok tokenResp
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&tok))
	resp.Body.Close()

	return &serversRig{
		srv:         srv,
		queries:     queries,
		bearer:      tok.AccessToken,
		fakeProv:    fake,
		riverClient: rt.Client(),
		logger:      logger,
	}
}

func TestCreateServerHappyPath(t *testing.T) {
	t.Parallel()
	rig := newServersRig(t)

	body := map[string]any{
		"name":         "vps1",
		"public_ip":    "203.0.113.10",
		"ssh_user":     "root",
		"ssh_port":     22,
		"ssh_password": "hunter2",
	}
	resp := postJSON(t, rig.srv.URL+"/v1/servers", body, rig.bearer)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	var created serverDTO
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	resp.Body.Close()
	require.NotZero(t, created.ID)
	require.Equal(t, "pending", created.Status)

	// Poll until status=ready (fake provisioner immediately stamps
	// agent_last_seen so the worker's wait loop returns fast).
	deadline := time.Now().Add(15 * time.Second)
	var got serverDTO
	for time.Now().Before(deadline) {
		r := getJSON(t, rig.srv.URL+"/v1/servers/"+itoa(created.ID), rig.bearer)
		require.Equal(t, http.StatusOK, r.StatusCode)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		r.Body.Close()
		if got.Status == "ready" {
			break
		}
		if got.Status == "error" {
			t.Fatalf("provisioning failed: %v", got.ProvisionError)
		}
		time.Sleep(100 * time.Millisecond)
	}
	require.Equal(t, "ready", got.Status, "provisioning did not reach ready")
	require.NotNil(t, got.AgentFingerprint)
	require.NotEmpty(t, *got.AgentFingerprint)

	// Provisioner was called exactly once with the expected target.
	calls := rig.fakeProv.Calls()
	require.Len(t, calls, 1)
	require.Equal(t, "203.0.113.10", calls[0].Host)
	require.Equal(t, 22, calls[0].Port)
	require.Equal(t, "root", calls[0].User)
	require.Equal(t, "hunter2", calls[0].Password)
}

func TestCreateServerProvisionFails(t *testing.T) {
	t.Parallel()
	rig := newServersRig(t)
	rig.fakeProv.SetOnCall(func(_ context.Context, _ provisioner.Target) error {
		return provisioner.ErrBootstrapFailed
	})

	body := map[string]any{
		"name": "vps2", "public_ip": "203.0.113.20",
		"ssh_user": "root", "ssh_port": 22, "ssh_password": "x",
	}
	resp := postJSON(t, rig.srv.URL+"/v1/servers", body, rig.bearer)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	var created serverDTO
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	resp.Body.Close()

	deadline := time.Now().Add(10 * time.Second)
	var got serverDTO
	for time.Now().Before(deadline) {
		r := getJSON(t, rig.srv.URL+"/v1/servers/"+itoa(created.ID), rig.bearer)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		r.Body.Close()
		if got.Status == "error" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	require.Equal(t, "error", got.Status)
	require.NotNil(t, got.ProvisionError)
	require.Contains(t, *got.ProvisionError, "bootstrap")
}

func TestCreateServerValidation(t *testing.T) {
	t.Parallel()
	rig := newServersRig(t)

	cases := map[string]map[string]any{
		"missing name":  {"public_ip": "203.0.113.1", "ssh_user": "root", "ssh_password": "x"},
		"missing ip":    {"name": "x", "ssh_user": "root", "ssh_password": "x"},
		"missing creds": {"name": "x", "public_ip": "203.0.113.1", "ssh_user": "root"},
		"both creds":    {"name": "x", "public_ip": "203.0.113.1", "ssh_user": "root", "ssh_password": "p", "ssh_private_key": "k"},
		"bad ip":        {"name": "x", "public_ip": "not-an-ip", "ssh_user": "root", "ssh_password": "x"},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			resp := postJSON(t, rig.srv.URL+"/v1/servers", body, rig.bearer)
			require.Equal(t, http.StatusBadRequest, resp.StatusCode)
			resp.Body.Close()
		})
	}
}

func TestListAndDeleteServer(t *testing.T) {
	t.Parallel()
	rig := newServersRig(t)

	body := map[string]any{
		"name": "vps3", "public_ip": "203.0.113.30",
		"ssh_user": "root", "ssh_port": 22, "ssh_password": "x",
	}
	resp := postJSON(t, rig.srv.URL+"/v1/servers", body, rig.bearer)
	var created serverDTO
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	resp.Body.Close()

	// List
	r := getJSON(t, rig.srv.URL+"/v1/servers", rig.bearer)
	require.Equal(t, http.StatusOK, r.StatusCode)
	var list []serverDTO
	require.NoError(t, json.NewDecoder(r.Body).Decode(&list))
	r.Body.Close()
	require.Len(t, list, 1)

	// Delete
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodDelete,
		rig.srv.URL+"/v1/servers/"+itoa(created.ID), nil)
	req.Header.Set("Authorization", "Bearer "+rig.bearer)
	dresp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, dresp.StatusCode)
	dresp.Body.Close()

	// Confirm gone
	r2 := getJSON(t, rig.srv.URL+"/v1/servers/"+itoa(created.ID), rig.bearer)
	require.Equal(t, http.StatusNotFound, r2.StatusCode)
	r2.Body.Close()
}

func itoa(i int64) string {
	return string(formatInt64(i))
}

// formatInt64 — strconv.Itoa takes int; we have int64. Avoid an extra
// import by doing the small base-10 conversion inline.
func formatInt64(n int64) []byte {
	if n == 0 {
		return []byte{'0'}
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return buf[i:]
}
