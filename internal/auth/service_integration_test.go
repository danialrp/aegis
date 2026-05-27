// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build integration

package auth_test

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/danialrp/aegis/internal/audit"
	"github.com/danialrp/aegis/internal/auth"
	"github.com/danialrp/aegis/internal/db/dbtest"
	"github.com/danialrp/aegis/internal/db/sqlc"
)

var (
	intSecret     = []byte("integration-test-secret-32-chars-x")
	intAccessTTL  = 15 * time.Minute
	intRefreshTTL = 24 * time.Hour
	intTestIP     = netip.MustParseAddr("203.0.113.1")
)

func newService(t *testing.T) (*auth.Service, *sqlc.Queries) {
	t.Helper()
	pool := dbtest.NewPostgres(t)
	q := sqlc.New(pool)
	rec := audit.New(q)
	svc := auth.NewService(pool, rec, intSecret, intAccessTTL, intRefreshTTL)
	return svc, q
}

func seedUser(t *testing.T, q *sqlc.Queries, email, password string, role auth.Role) *sqlc.User {
	t.Helper()
	hash, err := auth.HashPassword(password)
	require.NoError(t, err)
	u, err := q.CreateUser(context.Background(), sqlc.CreateUserParams{
		Email: email, PasswordHash: hash, Role: string(role), Enabled: true,
	})
	require.NoError(t, err)
	return &u
}

func TestLoginSuccess(t *testing.T) {
	t.Parallel()
	svc, q := newService(t)
	seedUser(t, q, "danial@example.com", "correct-horse-battery-staple", auth.RoleGod)

	res, user, err := svc.Login(context.Background(), "danial@example.com", "correct-horse-battery-staple", &intTestIP, "go-test/1.0")
	require.NoError(t, err)
	require.NotEmpty(t, res.AccessToken)
	require.NotEmpty(t, res.RefreshToken)
	require.True(t, res.ExpiresAt.After(time.Now()))
	require.Equal(t, "danial@example.com", user.Email)

	// The JWT must verify against the same secret and reference the same session.
	claims, err := auth.ParseAccess(intSecret, res.AccessToken)
	require.NoError(t, err)
	require.Equal(t, res.RefreshToken, claims.SessionID)
	require.Equal(t, "god", claims.Role)
}

func TestLoginWrongPassword(t *testing.T) {
	t.Parallel()
	svc, q := newService(t)
	seedUser(t, q, "danial@example.com", "correct-horse-battery-staple", auth.RoleGod)

	_, _, err := svc.Login(context.Background(), "danial@example.com", "wrong-password-xx", &intTestIP, "")
	require.ErrorIs(t, err, auth.ErrInvalidCredentials)
}

func TestLoginUnknownUser(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)

	_, _, err := svc.Login(context.Background(), "ghost@example.com", "correct-horse-battery-staple", &intTestIP, "")
	require.ErrorIs(t, err, auth.ErrInvalidCredentials)
}

func TestLoginDisabledAccount(t *testing.T) {
	t.Parallel()
	svc, q := newService(t)
	u := seedUser(t, q, "danial@example.com", "correct-horse-battery-staple", auth.RoleGod)
	require.NoError(t, q.SetUserEnabled(context.Background(), sqlc.SetUserEnabledParams{ID: u.ID, Enabled: false}))

	_, _, err := svc.Login(context.Background(), "danial@example.com", "correct-horse-battery-staple", &intTestIP, "")
	require.ErrorIs(t, err, auth.ErrAccountDisabled)
}

func TestVerifySessionAndLogout(t *testing.T) {
	t.Parallel()
	svc, q := newService(t)
	seedUser(t, q, "danial@example.com", "correct-horse-battery-staple", auth.RoleGod)

	res, user, err := svc.Login(context.Background(), "danial@example.com", "correct-horse-battery-staple", &intTestIP, "")
	require.NoError(t, err)

	got, err := svc.VerifySession(context.Background(), res.RefreshToken)
	require.NoError(t, err)
	require.Equal(t, user.ID, got.ID)

	require.NoError(t, svc.Logout(context.Background(), res.RefreshToken, user.ID, &intTestIP))

	_, err = svc.VerifySession(context.Background(), res.RefreshToken)
	require.ErrorIs(t, err, auth.ErrInvalidSession)
}

func TestRefreshRotatesSession(t *testing.T) {
	t.Parallel()
	svc, q := newService(t)
	seedUser(t, q, "danial@example.com", "correct-horse-battery-staple", auth.RoleGod)

	first, _, err := svc.Login(context.Background(), "danial@example.com", "correct-horse-battery-staple", &intTestIP, "")
	require.NoError(t, err)

	second, err := svc.Refresh(context.Background(), first.RefreshToken, &intTestIP, "")
	require.NoError(t, err)
	require.NotEqual(t, first.RefreshToken, second.RefreshToken, "session UUID must rotate")
	require.NotEqual(t, first.AccessToken, second.AccessToken, "access token must rotate")

	// Old refresh token must no longer work.
	_, err = svc.Refresh(context.Background(), first.RefreshToken, &intTestIP, "")
	require.ErrorIs(t, err, auth.ErrInvalidSession)

	// New session is verifiable.
	_, err = svc.VerifySession(context.Background(), second.RefreshToken)
	require.NoError(t, err)
}

func TestBootstrapGodCreatesOnEmptyDB(t *testing.T) {
	t.Parallel()
	svc, q := newService(t)

	require.NoError(t, svc.BootstrapGodIfMissing(context.Background(), "boot@example.com", "bootstrap-passphrase-here"))

	got, err := q.GetUserByEmail(context.Background(), "boot@example.com")
	require.NoError(t, err)
	require.Equal(t, "god", got.Role)
	require.True(t, got.Enabled)
}

func TestBootstrapGodIsIdempotent(t *testing.T) {
	t.Parallel()
	svc, q := newService(t)

	require.NoError(t, svc.BootstrapGodIfMissing(context.Background(), "boot@example.com", "bootstrap-passphrase-here"))
	require.NoError(t, svc.BootstrapGodIfMissing(context.Background(), "second@example.com", "bootstrap-passphrase-here"))

	users, err := q.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 1, "second bootstrap must be a no-op when a god already exists")
}

func TestBootstrapGodNoOpWhenUnconfigured(t *testing.T) {
	t.Parallel()
	svc, q := newService(t)

	require.NoError(t, svc.BootstrapGodIfMissing(context.Background(), "", ""))
	users, err := q.ListUsers(context.Background())
	require.NoError(t, err)
	require.Empty(t, users)
}
