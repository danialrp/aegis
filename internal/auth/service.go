// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/danialrp/aegis/internal/audit"
	"github.com/danialrp/aegis/internal/db/sqlc"
)

// Sentinel errors returned by the Service methods. Wrap with
// errors.Is at the handler boundary to map to HTTP status codes.
var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrAccountDisabled    = errors.New("account disabled")
	ErrInvalidSession     = errors.New("invalid session")
)

// Service is the auth domain entry point. It owns password
// verification, JWT minting, session lifecycle, and audit emission
// for auth events.
type Service struct {
	pool       *pgxpool.Pool
	queries    *sqlc.Queries
	audit      *audit.Recorder
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration

	nowFn func() time.Time // overridable for tests
}

// NewService wires a Service over the given dependencies.
func NewService(pool *pgxpool.Pool, rec *audit.Recorder, secret []byte, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{
		pool:       pool,
		queries:    sqlc.New(pool),
		audit:      rec,
		secret:     secret,
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
		nowFn:      time.Now,
	}
}

// LoginResult is what Login and Refresh return on success.
type LoginResult struct {
	AccessToken  string
	RefreshToken string // session UUID as string
	ExpiresAt    time.Time
}

// Login validates credentials and, on success, creates a new session
// row and returns an access JWT plus the session UUID (refresh token).
//
// Failures audit as auth.login.failed with a reason key; success
// audits as auth.login.success.
func (s *Service) Login(ctx context.Context, email, password string, ip *netip.Addr, userAgent string) (*LoginResult, *sqlc.User, error) {
	user, err := s.queries.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Equalize timing — verify against a dummy hash so attackers
			// can't distinguish "unknown user" from "wrong password" by
			// response latency.
			_, _ = VerifyPassword(password, DummyHash())
			_ = s.audit.Record(ctx, audit.Event{
				ActorIP: ip, Action: "auth.login.failed",
				Payload: map[string]string{"email": email, "reason": "user_not_found"},
			})
			return nil, nil, ErrInvalidCredentials
		}
		return nil, nil, fmt.Errorf("get user: %w", err)
	}

	ok, verr := VerifyPassword(password, user.PasswordHash)
	if verr != nil || !ok {
		_ = s.audit.Record(ctx, audit.Event{
			ActorUserID: &user.ID, ActorIP: ip, Action: "auth.login.failed",
			Payload: map[string]string{"reason": "bad_password"},
		})
		return nil, nil, ErrInvalidCredentials
	}

	if !user.Enabled {
		_ = s.audit.Record(ctx, audit.Event{
			ActorUserID: &user.ID, ActorIP: ip, Action: "auth.login.failed",
			Payload: map[string]string{"reason": "account_disabled"},
		})
		return nil, nil, ErrAccountDisabled
	}

	// Opportunistic rehash on successful login if the stored hash uses
	// older argon2id parameters than the current package defaults.
	if NeedsRehash(user.PasswordHash) {
		if newHash, hErr := HashPassword(password); hErr == nil {
			_ = s.queries.UpdateUserPassword(ctx, sqlc.UpdateUserPasswordParams{
				ID: user.ID, PasswordHash: newHash,
			})
		}
	}

	result, err := s.issueSession(ctx, &user, ip, userAgent)
	if err != nil {
		return nil, nil, err
	}

	_ = s.audit.Record(ctx, audit.Event{
		ActorUserID: &user.ID, ActorIP: ip, Action: "auth.login.success",
		TargetType: "session",
		Payload:    map[string]string{"session_id": result.RefreshToken},
	})

	return result, &user, nil
}

// Refresh consumes the given refresh token (a session UUID), atomically
// rotates the session row (delete + insert), and returns a fresh access
// JWT and refresh token. The previous refresh token is permanently
// invalid after a successful call.
func (s *Service) Refresh(ctx context.Context, refreshToken string, ip *netip.Addr, userAgent string) (*LoginResult, error) {
	sid, err := uuid.Parse(refreshToken)
	if err != nil {
		return nil, ErrInvalidSession
	}
	pgUUID := pgtype.UUID{Bytes: sid, Valid: true}

	var result *LoginResult
	txErr := s.withTx(ctx, func(q *sqlc.Queries) error {
		session, err := q.GetSession(ctx, pgUUID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrInvalidSession
			}
			return fmt.Errorf("get session: %w", err)
		}
		if !session.ExpiresAt.Valid || session.ExpiresAt.Time.Before(s.nowFn()) {
			return ErrInvalidSession
		}

		user, err := q.GetUserByID(ctx, session.UserID)
		if err != nil {
			return fmt.Errorf("get user: %w", err)
		}
		if !user.Enabled {
			return ErrAccountDisabled
		}

		if err := q.DeleteSession(ctx, pgUUID); err != nil {
			return fmt.Errorf("delete old session: %w", err)
		}

		issued, err := s.issueSessionTx(ctx, q, &user, ip, userAgent)
		if err != nil {
			return err
		}
		result = issued
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return result, nil
}

// Logout deletes the session row for sessionID, immediately
// invalidating any subsequent VerifySession calls (and therefore any
// access JWT that still references it).
func (s *Service) Logout(ctx context.Context, sessionID string, actorUserID int64, ip *netip.Addr) error {
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return ErrInvalidSession
	}
	if err := s.queries.DeleteSession(ctx, pgtype.UUID{Bytes: sid, Valid: true}); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	_ = s.audit.Record(ctx, audit.Event{
		ActorUserID: &actorUserID, ActorIP: ip, Action: "auth.logout",
		TargetType: "session", Payload: map[string]string{"session_id": sessionID},
	})
	return nil
}

// VerifySession looks up sessionID, checks expiry, and returns the
// owning user. Used by RequireAuth middleware on every authenticated
// request — so revocation via Logout is instant.
func (s *Service) VerifySession(ctx context.Context, sessionID string) (*sqlc.User, error) {
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return nil, ErrInvalidSession
	}
	session, err := s.queries.GetSession(ctx, pgtype.UUID{Bytes: sid, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidSession
		}
		return nil, fmt.Errorf("get session: %w", err)
	}
	if !session.ExpiresAt.Valid || session.ExpiresAt.Time.Before(s.nowFn()) {
		return nil, ErrInvalidSession
	}
	user, err := s.queries.GetUserByID(ctx, session.UserID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if !user.Enabled {
		return nil, ErrAccountDisabled
	}
	return &user, nil
}

// BootstrapGodIfMissing creates a god user with the given credentials
// if no god user exists yet. Intended for first-boot provisioning;
// safe to call on every startup. A no-op when either email or
// password is empty so operators can omit the env vars after first run.
func (s *Service) BootstrapGodIfMissing(ctx context.Context, email, password string) error {
	if email == "" || password == "" {
		return nil
	}
	if len(password) < MinPasswordLen {
		return fmt.Errorf("bootstrap password must be at least %d characters", MinPasswordLen)
	}

	users, err := s.queries.ListUsers(ctx)
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}
	for _, u := range users {
		if u.Role == string(RoleGod) {
			return nil
		}
	}

	hash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash bootstrap password: %w", err)
	}
	user, err := s.queries.CreateUser(ctx, sqlc.CreateUserParams{
		Email: email, PasswordHash: hash, Role: string(RoleGod), Enabled: true,
	})
	if err != nil {
		return fmt.Errorf("create god user: %w", err)
	}
	_ = s.audit.Record(ctx, audit.Event{
		ActorUserID: &user.ID, Action: "auth.bootstrap.god_created",
		Payload: map[string]string{"email": email},
	})
	return nil
}

// --- internals ---

func (s *Service) issueSession(ctx context.Context, user *sqlc.User, ip *netip.Addr, userAgent string) (*LoginResult, error) {
	return s.issueSessionTx(ctx, s.queries, user, ip, userAgent)
}

func (s *Service) issueSessionTx(ctx context.Context, q *sqlc.Queries, user *sqlc.User, ip *netip.Addr, userAgent string) (*LoginResult, error) {
	expiresAt := s.nowFn().Add(s.refreshTTL)
	session, err := q.CreateSession(ctx, sqlc.CreateSessionParams{
		UserID:    user.ID,
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
		Ip:        ip,
		UserAgent: textOrNull(userAgent),
	})
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	sessionID := uuid.UUID(session.ID.Bytes).String()
	token, err := MintAccess(s.secret, user.ID, sessionID, Role(user.Role), s.accessTTL)
	if err != nil {
		return nil, fmt.Errorf("mint access token: %w", err)
	}
	return &LoginResult{
		AccessToken:  token,
		RefreshToken: sessionID,
		ExpiresAt:    expiresAt,
	}, nil
}

func (s *Service) withTx(ctx context.Context, fn func(*sqlc.Queries) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(s.queries.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func textOrNull(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}
