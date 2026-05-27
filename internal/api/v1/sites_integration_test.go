// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build integration

package v1_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/danialrp/aegis/internal/api"
	"github.com/danialrp/aegis/internal/audit"
	"github.com/danialrp/aegis/internal/auth"
	"github.com/danialrp/aegis/internal/db/dbtest"
	"github.com/danialrp/aegis/internal/db/sqlc"
)

var sitesSecret = []byte("sites-integration-secret-32-chrs")

type siteDTO struct {
	ID         int64   `json:"id"`
	ServerID   int64   `json:"server_id"`
	Name       string  `json:"name"`
	Domain     string  `json:"domain"`
	SiteType   string  `json:"site_type"`
	Status     string  `json:"status"`
	WorkingDir string  `json:"working_dir"`
	ProvError  *string `json:"provision_error,omitempty"`
}

type sitesRig struct {
	srv      *httptest.Server
	queries  *sqlc.Queries
	bearer   string
	serverID int64
}

func newSitesRig(t *testing.T) *sitesRig {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool := dbtest.NewPostgres(t)
	queries := sqlc.New(pool)
	rec := audit.New(queries)

	authSvc := auth.NewService(pool, rec, sitesSecret, 15*time.Minute, 24*time.Hour)
	handler := api.NewRouter(api.Deps{
		Logger:      logger,
		Pool:        pool,
		AuthService: authSvc,
		JWTSecret:   sitesSecret,
		Queries:     queries,
		Audit:       rec,
		// RiverClient intentionally nil — sites endpoints don't enqueue
		// jobs in 1.1.
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	// Seed a god user + a server row so we have a parent for sites.
	hash, err := auth.HashPassword("correct-horse-battery-staple")
	require.NoError(t, err)
	_, err = queries.CreateUser(context.Background(), sqlc.CreateUserParams{
		Email: "admin@example.com", PasswordHash: hash, Role: "god", Enabled: true,
	})
	require.NoError(t, err)

	addr := netip.MustParseAddr("203.0.113.50")
	srvRow, err := queries.CreateServer(context.Background(), sqlc.CreateServerParams{
		Name:            "host-1",
		PublicIp:        addr,
		SshUser:         "root",
		ProvisionStatus: "ready",
	})
	require.NoError(t, err)

	// Log in to get a bearer.
	resp := postJSON(t, srv.URL+"/v1/auth/login", map[string]string{
		"email": "admin@example.com", "password": "correct-horse-battery-staple",
	}, "")
	var tok tokenResp
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&tok))
	resp.Body.Close()

	return &sitesRig{
		srv:      srv,
		queries:  queries,
		bearer:   tok.AccessToken,
		serverID: srvRow.ID,
	}
}

func TestCreateSiteHappyPath(t *testing.T) {
	t.Parallel()
	rig := newSitesRig(t)

	body := map[string]any{
		"server_id": rig.serverID,
		"name":      "marketing",
		"domain":    "marketing.example.com",
		"site_type": "static",
	}
	resp := postJSON(t, rig.srv.URL+"/v1/sites", body, rig.bearer)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var got siteDTO
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	resp.Body.Close()
	require.NotZero(t, got.ID)
	require.Equal(t, rig.serverID, got.ServerID)
	require.Equal(t, "static", got.SiteType)
	require.Equal(t, "pending", got.Status)
	require.Equal(t, "/srv/sites/"+itoa(got.ID), got.WorkingDir,
		"working_dir must reflect the new id, not the placeholder")
}

func TestCreateSiteValidation(t *testing.T) {
	t.Parallel()
	rig := newSitesRig(t)

	cases := map[string]map[string]any{
		"missing server": {"name": "x", "domain": "x.example", "site_type": "static"},
		"missing name":   {"server_id": rig.serverID, "domain": "x.example", "site_type": "static"},
		"missing domain": {"server_id": rig.serverID, "name": "x", "site_type": "static"},
		"invalid type":   {"server_id": rig.serverID, "name": "x", "domain": "x.example", "site_type": "rust"},
		"unknown server": {"server_id": 99999, "name": "x", "domain": "x.example", "site_type": "static"},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			resp := postJSON(t, rig.srv.URL+"/v1/sites", body, rig.bearer)
			require.Equal(t, http.StatusBadRequest, resp.StatusCode)
			resp.Body.Close()
		})
	}
}

func TestSiteDomainUniquePerServer(t *testing.T) {
	t.Parallel()
	rig := newSitesRig(t)

	first := map[string]any{
		"server_id": rig.serverID, "name": "a",
		"domain": "shared.example.com", "site_type": "static",
	}
	resp := postJSON(t, rig.srv.URL+"/v1/sites", first, rig.bearer)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	resp.Body.Close()

	second := map[string]any{
		"server_id": rig.serverID, "name": "b",
		"domain": "shared.example.com", "site_type": "static",
	}
	resp = postJSON(t, rig.srv.URL+"/v1/sites", second, rig.bearer)
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	resp.Body.Close()
}

func TestListAndDeleteSite(t *testing.T) {
	t.Parallel()
	rig := newSitesRig(t)

	resp := postJSON(t, rig.srv.URL+"/v1/sites", map[string]any{
		"server_id": rig.serverID, "name": "to-delete",
		"domain": "del.example.com", "site_type": "static",
	}, rig.bearer)
	var created siteDTO
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	resp.Body.Close()

	r := getJSON(t, rig.srv.URL+"/v1/sites", rig.bearer)
	require.Equal(t, http.StatusOK, r.StatusCode)
	var list []siteDTO
	require.NoError(t, json.NewDecoder(r.Body).Decode(&list))
	r.Body.Close()
	require.Len(t, list, 1)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodDelete,
		rig.srv.URL+"/v1/sites/"+itoa(created.ID), nil)
	req.Header.Set("Authorization", "Bearer "+rig.bearer)
	dresp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, dresp.StatusCode)
	dresp.Body.Close()

	r2 := getJSON(t, rig.srv.URL+"/v1/sites/"+itoa(created.ID), rig.bearer)
	require.Equal(t, http.StatusNotFound, r2.StatusCode)
	r2.Body.Close()
}

// TestDeleteServerCascadesSites verifies that the FK ON DELETE CASCADE
// from sites to servers wipes a server's sites when the server is removed.
func TestDeleteServerCascadesSites(t *testing.T) {
	t.Parallel()
	rig := newSitesRig(t)

	resp := postJSON(t, rig.srv.URL+"/v1/sites", map[string]any{
		"server_id": rig.serverID, "name": "cascading",
		"domain": "cascade.example.com", "site_type": "static",
	}, rig.bearer)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	resp.Body.Close()

	// Sanity: site exists.
	rows, err := rig.queries.ListSites(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)

	// Delete the parent server directly via queries (the HTTP handler
	// works too, but we don't have a river client wired here).
	require.NoError(t, rig.queries.DeleteServer(context.Background(), rig.serverID))

	rows, err = rig.queries.ListSites(context.Background())
	require.NoError(t, err)
	require.Empty(t, rows, "deleting the server must cascade to its sites")
}
