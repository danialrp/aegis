// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build integration

package api_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/danialrp/aegis/internal/api"
	"github.com/danialrp/aegis/internal/db/dbtest"
)

func TestReadyzWithRealPostgres(t *testing.T) {
	t.Parallel()

	pool := dbtest.NewPostgres(t)
	handler := api.NewRouter(api.Deps{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Pool:   pool,
	})

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"status":"ready"}`, rec.Body.String())
}

func TestReadyzWithClosedPool(t *testing.T) {
	t.Parallel()

	pool := dbtest.NewPostgres(t)
	pool.Close() // simulate the source of truth being unreachable

	handler := api.NewRouter(api.Deps{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Pool:   pool,
	})

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
