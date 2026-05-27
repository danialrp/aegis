// SPDX-License-Identifier: AGPL-3.0-or-later

package auth_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/danialrp/aegis/internal/auth"
)

func TestRoleValid(t *testing.T) {
	t.Parallel()

	require.True(t, auth.RoleGod.Valid())
	require.True(t, auth.RoleAdmin.Valid())
	require.True(t, auth.RoleSiteUser.Valid())
	require.False(t, auth.Role("").Valid())
	require.False(t, auth.Role("root").Valid())
}
