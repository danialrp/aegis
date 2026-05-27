// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AccessClaims are the JWT body of an access token.
//
// The refresh "token" is the raw session UUID — it is opaque to the
// client and validated server-side against the sessions table. No JWT
// is used for refresh.
type AccessClaims struct {
	SessionID string `json:"sid"`
	Role      string `json:"role"`
	jwt.RegisteredClaims
}

// Sentinel errors returned by ParseAccess.
var (
	ErrInvalidToken = errors.New("invalid token")
	ErrTokenExpired = errors.New("token expired")
)

// MintAccess produces a signed HS256 JWT carrying the user id, the
// session id (UUID string), and the user's role.
func MintAccess(secret []byte, userID int64, sessionID string, role Role, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := AccessClaims{
		SessionID: sessionID,
		Role:      string(role),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(userID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(secret)
	if err != nil {
		return "", fmt.Errorf("sign jwt: %w", err)
	}
	return signed, nil
}

// ParseAccess verifies the signature, checks exp/nbf, and returns the
// embedded claims. Returns ErrTokenExpired specifically for expiry so
// callers can react (e.g. prompt for refresh).
func ParseAccess(secret []byte, tokenString string) (*AccessClaims, error) {
	parsed, err := jwt.ParseWithClaims(tokenString, &AccessClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	claims, ok := parsed.Claims.(*AccessClaims)
	if !ok || !parsed.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// UserIDFromClaims extracts the numeric user id from the Subject claim.
func (c *AccessClaims) UserIDFromClaims() (int64, error) {
	id, err := strconv.ParseInt(c.Subject, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: subject not int64", ErrInvalidToken)
	}
	return id, nil
}
