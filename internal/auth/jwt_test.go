// SPDX-License-Identifier: AGPL-3.0-or-later

package auth_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/danialrp/aegis/internal/auth"
)

var testSecret = []byte("super-secret-test-key-please-do-not-leak")

func TestJWTMintParseRoundTrip(t *testing.T) {
	t.Parallel()

	token, err := auth.MintAccess(testSecret, 42, "11111111-1111-1111-1111-111111111111", auth.RoleGod, time.Hour)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	claims, err := auth.ParseAccess(testSecret, token)
	require.NoError(t, err)
	require.Equal(t, "42", claims.Subject)
	require.Equal(t, "11111111-1111-1111-1111-111111111111", claims.SessionID)
	require.Equal(t, "god", claims.Role)

	uid, err := claims.UserIDFromClaims()
	require.NoError(t, err)
	require.Equal(t, int64(42), uid)
}

func TestJWTExpired(t *testing.T) {
	t.Parallel()

	token, err := auth.MintAccess(testSecret, 1, "sid", auth.RoleSiteUser, -time.Minute)
	require.NoError(t, err)

	_, err = auth.ParseAccess(testSecret, token)
	require.ErrorIs(t, err, auth.ErrTokenExpired)
}

func TestJWTWrongSecret(t *testing.T) {
	t.Parallel()

	token, err := auth.MintAccess(testSecret, 1, "sid", auth.RoleSiteUser, time.Hour)
	require.NoError(t, err)

	_, err = auth.ParseAccess([]byte("different-secret-32-chars-long-x"), token)
	require.ErrorIs(t, err, auth.ErrInvalidToken)
}

func TestJWTMalformed(t *testing.T) {
	t.Parallel()

	_, err := auth.ParseAccess(testSecret, "not.a.jwt")
	require.ErrorIs(t, err, auth.ErrInvalidToken)
}
