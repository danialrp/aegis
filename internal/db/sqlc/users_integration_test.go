// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build integration

package sqlc_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/danialrp/aegis/internal/db/dbtest"
	"github.com/danialrp/aegis/internal/db/sqlc"
)

// TestUsersRoundTrip is the end-to-end smoke test for the 0.3 stack:
// testcontainers boots Postgres → goose applies migrations → sqlc
// queries write and read a row back. If this passes, the toolchain
// is wired correctly.
func TestUsersRoundTrip(t *testing.T) {
	t.Parallel()

	pool := dbtest.NewPostgres(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	created, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		Email:        "danial@example.com",
		PasswordHash: "argon2id$placeholder",
		Role:         "god",
		Enabled:      true,
	})
	require.NoError(t, err)
	require.NotZero(t, created.ID)
	require.Equal(t, "danial@example.com", created.Email)
	require.Equal(t, "god", created.Role)
	require.True(t, created.Enabled)

	fetched, err := q.GetUserByEmail(ctx, "danial@example.com")
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.ID)
	require.Equal(t, created.PasswordHash, fetched.PasswordHash)
}
