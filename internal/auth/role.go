// SPDX-License-Identifier: AGPL-3.0-or-later

// Package auth owns the controller's credentials, JWT minting, session
// lifecycle, and role enum. It also serves as the boundary that the
// audit recorder is invoked from for every successful or failed login.
package auth

// Role is the panel-level role assigned to a user.
//
// The same string values are enforced by a CHECK constraint in the
// users table — the Go-side enum exists for input validation at API
// boundaries and for type safety in handlers.
type Role string

// The three known role values. Mirrors the CHECK constraint on
// users.role; any change here needs a matching migration.
const (
	RoleGod      Role = "god"
	RoleAdmin    Role = "admin"
	RoleSiteUser Role = "site_user"
)

// Valid reports whether r is one of the three known role values.
func (r Role) Valid() bool {
	switch r {
	case RoleGod, RoleAdmin, RoleSiteUser:
		return true
	default:
		return false
	}
}
