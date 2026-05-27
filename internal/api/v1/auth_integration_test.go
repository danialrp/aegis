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

	"github.com/stretchr/testify/require"

	"github.com/danialrp/aegis/internal/api"
	"github.com/danialrp/aegis/internal/audit"
	"github.com/danialrp/aegis/internal/auth"
	"github.com/danialrp/aegis/internal/db/dbtest"
	"github.com/danialrp/aegis/internal/db/sqlc"
)

var httpSecret = []byte("v1-integration-secret-32-chars-x")

type tokenResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"expires_at"`
}

type meResp struct {
	ID      int64  `json:"id"`
	Email   string `json:"email"`
	Role    string `json:"role"`
	Enabled bool   `json:"enabled"`
}

func newServer(t *testing.T) (*httptest.Server, *sqlc.Queries) {
	t.Helper()
	pool := dbtest.NewPostgres(t)
	q := sqlc.New(pool)
	rec := audit.New(q)
	svc := auth.NewService(pool, rec, httpSecret, 15*time.Minute, 24*time.Hour)

	handler := api.NewRouter(api.Deps{
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Pool:        pool,
		AuthService: svc,
		JWTSecret:   httpSecret,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, q
}

func postJSON(t *testing.T, url string, body any, bearer string) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(b))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func getJSON(t *testing.T, url, bearer string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func seedHTTPUser(t *testing.T, q *sqlc.Queries) {
	t.Helper()
	hash, err := auth.HashPassword("correct-horse-battery-staple")
	require.NoError(t, err)
	_, err = q.CreateUser(context.Background(), sqlc.CreateUserParams{
		Email: "danial@example.com", PasswordHash: hash, Role: "god", Enabled: true,
	})
	require.NoError(t, err)
}

func TestFullLoginMeLogoutFlow(t *testing.T) {
	t.Parallel()
	srv, q := newServer(t)
	seedHTTPUser(t, q)

	// Login
	resp := postJSON(t, srv.URL+"/v1/auth/login", map[string]string{
		"email": "danial@example.com", "password": "correct-horse-battery-staple",
	}, "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var tokens tokenResp
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&tokens))
	resp.Body.Close()
	require.NotEmpty(t, tokens.AccessToken)
	require.NotEmpty(t, tokens.RefreshToken)

	// /me with valid token
	resp = getJSON(t, srv.URL+"/v1/auth/me", tokens.AccessToken)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var me meResp
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&me))
	resp.Body.Close()
	require.Equal(t, "danial@example.com", me.Email)
	require.Equal(t, "god", me.Role)

	// Logout
	resp = postJSON(t, srv.URL+"/v1/auth/logout", struct{}{}, tokens.AccessToken)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	resp.Body.Close()

	// /me must now fail — session is gone server-side even though JWT exp is in the future.
	resp = getJSON(t, srv.URL+"/v1/auth/me", tokens.AccessToken)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	resp.Body.Close()
}

func TestMeRequiresAuth(t *testing.T) {
	t.Parallel()
	srv, _ := newServer(t)

	resp := getJSON(t, srv.URL+"/v1/auth/me", "")
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	resp.Body.Close()
}

func TestLoginInvalidCredentials(t *testing.T) {
	t.Parallel()
	srv, q := newServer(t)
	seedHTTPUser(t, q)

	resp := postJSON(t, srv.URL+"/v1/auth/login", map[string]string{
		"email": "danial@example.com", "password": "wrong-password-xx",
	}, "")
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	resp.Body.Close()
}

func TestRefreshRotates(t *testing.T) {
	t.Parallel()
	srv, q := newServer(t)
	seedHTTPUser(t, q)

	resp := postJSON(t, srv.URL+"/v1/auth/login", map[string]string{
		"email": "danial@example.com", "password": "correct-horse-battery-staple",
	}, "")
	var first tokenResp
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&first))
	resp.Body.Close()

	resp = postJSON(t, srv.URL+"/v1/auth/refresh", map[string]string{
		"refresh_token": first.RefreshToken,
	}, "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var second tokenResp
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&second))
	resp.Body.Close()
	require.NotEqual(t, first.RefreshToken, second.RefreshToken)

	// Old refresh token must no longer work.
	resp = postJSON(t, srv.URL+"/v1/auth/refresh", map[string]string{
		"refresh_token": first.RefreshToken,
	}, "")
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	resp.Body.Close()
}

func TestLoginRateLimitTrips(t *testing.T) {
	t.Parallel()
	srv, _ := newServer(t)

	// burst=5: the 6th attempt within the same window must 429.
	var lastStatus int
	for range 6 {
		resp := postJSON(t, srv.URL+"/v1/auth/login", map[string]string{
			"email": "nobody@example.com", "password": "anything-here-12",
		}, "")
		lastStatus = resp.StatusCode
		resp.Body.Close()
	}
	require.Equal(t, http.StatusTooManyRequests, lastStatus)
}
