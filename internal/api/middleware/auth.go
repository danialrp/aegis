// SPDX-License-Identifier: AGPL-3.0-or-later

// Package middleware contains HTTP middleware specific to the
// controller's API — auth, rate limiting, etc.
package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/danialrp/aegis/internal/auth"
	"github.com/danialrp/aegis/internal/db/sqlc"
)

type contextKey string

const (
	userContextKey      contextKey = "auth.user"
	sessionIDContextKey contextKey = "auth.session_id"
)

// RequireAuth parses the bearer token, verifies its signature, looks
// up the referenced session row (instant-revocation by design), and
// attaches the user + session id to the request context.
func RequireAuth(svc *auth.Service, secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer ") {
				writeJSONStatus(w, http.StatusUnauthorized, `{"error":"unauthorized"}`)
				return
			}
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

			claims, err := auth.ParseAccess(secret, tokenStr)
			if err != nil {
				writeJSONStatus(w, http.StatusUnauthorized, `{"error":"unauthorized"}`)
				return
			}

			user, err := svc.VerifySession(r.Context(), claims.SessionID)
			if err != nil {
				writeJSONStatus(w, http.StatusUnauthorized, `{"error":"unauthorized"}`)
				return
			}

			ctx := context.WithValue(r.Context(), userContextKey, user)
			ctx = context.WithValue(ctx, sessionIDContextKey, claims.SessionID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole runs after RequireAuth and 403s requests whose user
// does not hold one of the given roles.
func RequireRole(roles ...auth.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFromContext(r.Context())
			if !ok {
				writeJSONStatus(w, http.StatusUnauthorized, `{"error":"unauthorized"}`)
				return
			}
			for _, role := range roles {
				if user.Role == string(role) {
					next.ServeHTTP(w, r)
					return
				}
			}
			writeJSONStatus(w, http.StatusForbidden, `{"error":"forbidden"}`)
		})
	}
}

// UserFromContext returns the authenticated user attached by
// RequireAuth, or (nil, false) if unauthenticated.
func UserFromContext(ctx context.Context) (*sqlc.User, bool) {
	u, ok := ctx.Value(userContextKey).(*sqlc.User)
	return u, ok
}

// SessionIDFromContext returns the session UUID string attached by
// RequireAuth, or ("", false) if unauthenticated.
func SessionIDFromContext(ctx context.Context) (string, bool) {
	s, ok := ctx.Value(sessionIDContextKey).(string)
	return s, ok
}

func writeJSONStatus(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
