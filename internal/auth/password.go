// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters. Tuned for ~100 ms on commodity x86 hardware per
// SECURITY.md. Bumping any of these is forward-compatible: NeedsRehash
// will report true for old hashes and Login will silently re-hash.
const (
	argonMemory      uint32 = 64 * 1024 // 64 MiB
	argonIterations  uint32 = 3
	argonParallelism uint8  = 2
	argonSaltLen     uint32 = 16
	argonKeyLen      uint32 = 32

	// MinPasswordLen is the floor; argon2id handles the rest. Follows
	// modern NIST guidance (length over complexity rules).
	MinPasswordLen = 12
)

// Sentinel errors returned by HashPassword and VerifyPassword.
var (
	ErrPasswordTooShort  = errors.New("password too short")
	ErrInvalidHashFormat = errors.New("invalid hash format")
	ErrIncompatibleAlgo  = errors.New("incompatible password hash algorithm")
)

// HashPassword produces a PHC-formatted argon2id hash. Returns
// ErrPasswordTooShort for inputs below MinPasswordLen.
func HashPassword(password string) (string, error) {
	if len(password) < MinPasswordLen {
		return "", ErrPasswordTooShort
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("read salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory, argonIterations, argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifyPassword reports whether password matches the encoded PHC
// hash. Returns ErrIncompatibleAlgo for non-argon2id hashes and
// ErrInvalidHashFormat for unparseable inputs.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, ErrIncompatibleAlgo
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, ErrInvalidHashFormat
	}
	var mem, iter uint32
	var par uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &iter, &par); err != nil {
		return false, ErrInvalidHashFormat
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrInvalidHashFormat
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, ErrInvalidHashFormat
	}

	if len(hash) < 1 || len(hash) > 1024 {
		return false, ErrInvalidHashFormat
	}
	// gosec G115: len(hash) is bounded above to 1024 by the guard
	// immediately above, so this conversion is provably safe.
	keyLen := uint32(len(hash)) //nolint:gosec // bounded above by guard
	computed := argon2.IDKey([]byte(password), salt, iter, mem, par, keyLen)
	return subtle.ConstantTimeCompare(hash, computed) == 1, nil
}

// NeedsRehash reports whether the encoded hash uses parameters that
// differ from the current package defaults — typically because the
// defaults were bumped after the password was originally set.
func NeedsRehash(encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		return true
	}
	var mem, iter uint32
	var par uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &iter, &par); err != nil {
		return true
	}
	return mem != argonMemory || iter != argonIterations || par != argonParallelism
}

// dummyHash is a precomputed argon2id hash used to equalize the
// timing of failed login attempts where the user simply does not
// exist. Generated lazily on first use so packages that import auth
// but never call Login (e.g. middleware-only consumers) don't pay
// the ~100 ms init cost.
var (
	dummyHashOnce sync.Once
	dummyHashVal  string
)

// DummyHash returns a stable, valid argon2id hash for timing-attack
// mitigation in code paths that need to fake a verification.
func DummyHash() string {
	dummyHashOnce.Do(func() {
		h, err := HashPassword("dummy-password-of-sufficient-length")
		if err != nil {
			// HashPassword can only fail on entropy errors; if it does,
			// fall back to a static well-formed string. VerifyPassword on
			// real input will still take its full argon2id time.
			h = "$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		}
		dummyHashVal = h
	})
	return dummyHashVal
}
