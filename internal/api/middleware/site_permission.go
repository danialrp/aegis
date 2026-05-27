// SPDX-License-Identifier: AGPL-3.0-or-later

package middleware

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/danialrp/aegis/internal/auth"
	"github.com/danialrp/aegis/internal/db/sqlc"
)

// SitePermissionFlag is the abstract capability a route requires the
// caller to hold on the site identified by the {id} URL parameter.
type SitePermissionFlag int

// The six per-site capability flags from ARCHITECTURE.md.
const (
	PermRead SitePermissionFlag = iota
	PermExecute
	PermWrite
	PermLogs
	PermTerminal
	PermInspect
)

// RequireSitePermission returns a middleware that lets the request
// through only if the authenticated caller is god/admin OR holds the
// required flag on the site referenced by the {id} URL parameter
// (directly or via team membership).
//
// The middleware assumes RequireAuth has already populated the
// request's user context. Site id is parsed from {id} (the canonical
// parameter name for /v1/sites/{id}/* routes).
func RequireSitePermission(queries *sqlc.Queries, flag SitePermissionFlag) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFromContext(r.Context())
			if !ok || user == nil {
				writeJSONStatus(w, http.StatusUnauthorized, `{"error":"unauthorized"}`)
				return
			}
			// god + admin always pass.
			if user.Role == string(auth.RoleGod) || user.Role == string(auth.RoleAdmin) {
				next.ServeHTTP(w, r)
				return
			}
			siteIDStr := chi.URLParam(r, "id")
			siteID, err := strconv.ParseInt(siteIDStr, 10, 64)
			if err != nil {
				writeJSONStatus(w, http.StatusBadRequest, `{"error":"invalid_site_id"}`)
				return
			}
			row, err := queries.SitePermissionsForUser(r.Context(), sqlc.SitePermissionsForUserParams{
				SiteID: siteID,
				UserID: pgtype.Int8{Int64: user.ID, Valid: true},
			})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					writeJSONStatus(w, http.StatusForbidden, `{"error":"forbidden"}`)
					return
				}
				writeJSONStatus(w, http.StatusInternalServerError, `{"error":"internal_error"}`)
				return
			}
			if !hasFlag(row, flag) {
				writeJSONStatus(w, http.StatusForbidden, `{"error":"forbidden"}`)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func hasFlag(row sqlc.SitePermissionsForUserRow, flag SitePermissionFlag) bool {
	switch flag {
	case PermRead:
		return row.PermRead
	case PermExecute:
		return row.PermExecute
	case PermWrite:
		return row.PermWrite
	case PermLogs:
		return row.PermLogs
	case PermTerminal:
		return row.PermTerminal
	case PermInspect:
		return row.PermInspect
	default:
		return false
	}
}
