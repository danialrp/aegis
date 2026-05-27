// SPDX-License-Identifier: AGPL-3.0-or-later

package auth_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/danialrp/aegis/internal/auth"
)

func TestHashAndVerifyRoundTrip(t *testing.T) {
	t.Parallel()

	hash, err := auth.HashPassword("correct-horse-battery-staple")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(hash, "$argon2id$"))

	ok, err := auth.VerifyPassword("correct-horse-battery-staple", hash)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = auth.VerifyPassword("wrong-password-here-12", hash)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestHashRejectsShortPassword(t *testing.T) {
	t.Parallel()

	_, err := auth.HashPassword("short")
	require.ErrorIs(t, err, auth.ErrPasswordTooShort)
}

func TestVerifyRejectsMalformed(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"empty":           "",
		"wrong algo":      "$bcrypt$v=19$m=65536,t=3,p=2$AAAA$BBBB",
		"truncated":       "$argon2id$v=19$m=65536,t=3,p=2$AAAA",
		"bad params":      "$argon2id$v=19$nonsense$AAAA$BBBB",
		"bad salt base64": "$argon2id$v=19$m=65536,t=3,p=2$!!!!$BBBB",
	}
	for name, encoded := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := auth.VerifyPassword("anything-at-all-12", encoded)
			require.Error(t, err)
		})
	}
}

func TestNeedsRehash(t *testing.T) {
	t.Parallel()

	hash, err := auth.HashPassword("correct-horse-battery-staple")
	require.NoError(t, err)
	require.False(t, auth.NeedsRehash(hash), "freshly hashed password must not need rehash")

	// Old parameters → should need rehash.
	require.True(t, auth.NeedsRehash("$argon2id$v=19$m=32768,t=2,p=1$AAAA$BBBB"))
	require.True(t, auth.NeedsRehash("garbage"))
}

func TestDummyHashIsStableAndVerifiable(t *testing.T) {
	t.Parallel()

	h1 := auth.DummyHash()
	h2 := auth.DummyHash()
	require.Equal(t, h1, h2, "DummyHash must be cached")
	require.True(t, strings.HasPrefix(h1, "$argon2id$"))
}
