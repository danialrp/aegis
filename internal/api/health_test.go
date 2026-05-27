// SPDX-License-Identifier: AGPL-3.0-or-later

package api_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/danialrp/aegis/internal/api"
)

func TestHealthz(t *testing.T) {
	t.Parallel()

	// /healthz must not touch the pool, so Pool is intentionally nil.
	handler := api.NewRouter(api.Deps{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}
