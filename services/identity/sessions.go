// sessions.go — the half of the store that holds sessions and refresh tokens (ADR-009).
//
// A session is the chain of refresh tokens that starts at one sign-in. It ends when its hard
// cap is reached, when it is revoked, or when a retired refresh token is presented again —
// which means a copy exists, so everything descended from that sign-in is thrown away.
package main

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The ways signing in or refreshing can fail that are the caller's doing.
var (
	ErrInvalidCredentials     = errors.New("email or password is wrong")
	ErrPasswordChangeRequired = errors.New("the temporary password must be replaced before signing in")
	ErrInvalidRefresh         = errors.New("refresh token is not valid")
	ErrSessionEnded           = errors.New("session has ended; sign in again")
	ErrTokenReused            = errors.New("refresh token was already used; the session has been revoked")
)

// credentials is what signing in needs out of the members table. The password hash is kept
// apart from Member on purpose: Member is what the API returns, and a hash must never be in it.
type credentials struct {
	member       Member
	passwordHash string
}

// FindCredentials returns an active member by email, with the hash to check a password against.
// A removed member is not found: removal ends the ability to sign in (FR-6.2).
func (s *Store) FindCredentials(ctx context.Context, email string) (credentials, error) {
	var c credentials
	row := s.db.QueryRow(ctx,
		`SELECT `+memberColumns+`, password_hash FROM members WHERE email = $1 AND removed_at IS NULL`, email)
	err := row.Scan(&c.member.ID, &c.member.Email, &c.member.Role, &c.member.TemporaryPassword,
		&c.member.CreatedAt, &c.member.UpdatedAt, &c.member.RemovedAt, &c.passwordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return credentials{}, ErrNotFound
	}
	return c, err
}

// StartSession records a sign-in: one session row with its hard expiry, and the first refresh
// token of the chain, stored as a hash. It also drops sessions that ended more than a day ago —
// at tens of members that is cheaper than a scheduled sweep, and a session nobody can use is
// only kept at all so a reused token is still recognised shortly afterwards.
func (s *Store) StartSession(ctx context.Context, memberID string, endsAt time.Time, tokenHash []byte) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE ends_at < now() - interval '1 day'`); err != nil {
			return err
		}
		var sessionID string
		err := tx.QueryRow(ctx, `
			INSERT INTO sessions (member_id, ends_at) VALUES ($1, $2) RETURNING id::text`,
			memberID, endsAt).Scan(&sessionID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO refresh_tokens (token_hash, session_id) VALUES ($1, $2)`, tokenHash, sessionID)
		return err
	})
}

// rotation is what a successful refresh yields: who the new access token is for, and when the
// session it belongs to ends however often it is refreshed.
type rotation struct {
	MemberID      string
	Role          string
	SessionEndsAt time.Time
}

// RotateRefreshToken exchanges one refresh token for the next. The presented token is retired
// in the same transaction that issues its replacement, so a token can be exchanged exactly once.
//
// The role is read from the members table rather than carried along, which is what makes a role
// change reach the next access token.
func (s *Store) RotateRefreshToken(ctx context.Context, presented []byte, now time.Time, next []byte) (rotation, error) {
	var (
		out rotation
		// refused is why the caller cannot have a new token. It is kept apart from the
		// transaction's own error because a refusal still has something to commit: finding a
		// reused token is the moment the session is revoked, and returning an error from the
		// transaction would roll that revocation back with it.
		refused error
	)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var (
			sessionID string
			usedAt    *time.Time
			revokedAt *time.Time
			endsAt    time.Time
			memberID  string
			role      string
			removedAt *time.Time
		)
		err := tx.QueryRow(ctx, `
			SELECT t.session_id::text, t.used_at, s.revoked_at, s.ends_at,
			       m.id::text, m.role, m.removed_at
			FROM refresh_tokens t
			JOIN sessions s ON s.id = t.session_id
			JOIN members  m ON m.id = s.member_id
			WHERE t.token_hash = $1
			FOR UPDATE OF t, s`, presented).
			Scan(&sessionID, &usedAt, &revokedAt, &endsAt, &memberID, &role, &removedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			refused = ErrInvalidRefresh
			return nil
		}
		if err != nil {
			return err
		}

		switch {
		// A token presented twice means two holders have the same token, and only one of them
		// is the member. There is no way to tell which, so the session goes.
		case usedAt != nil:
			refused = ErrTokenReused
			return revokeSession(ctx, tx, sessionID, "refresh token reused")
		case revokedAt != nil, !endsAt.After(now):
			refused = ErrSessionEnded
			return nil
		case removedAt != nil:
			refused = ErrSessionEnded
			return revokeSession(ctx, tx, sessionID, "member removed")
		}

		if _, err := tx.Exec(ctx,
			`UPDATE refresh_tokens SET used_at = $2 WHERE token_hash = $1`, presented, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO refresh_tokens (token_hash, session_id) VALUES ($1, $2)`, next, sessionID); err != nil {
			return err
		}
		out = rotation{MemberID: memberID, Role: role, SessionEndsAt: endsAt}
		return nil
	})
	if err != nil {
		return rotation{}, err
	}
	return out, refused
}

// RevokeSessions ends every session of one member, in the one transaction, and reports how many
// it ended. A new password or a removal takes effect here rather than at the next expiry.
func (s *Store) RevokeSessions(ctx context.Context, memberID, reason string) (int64, error) {
	tag, err := s.db.Exec(ctx, `
		UPDATE sessions SET revoked_at = now(), revoked_reason = $2
		WHERE member_id = $1 AND revoked_at IS NULL AND ends_at > now()`, memberID, reason)
	if err != nil {
		return 0, err
	}
	if err := deleteUnusedTokens(ctx, s.db, memberID); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// revokeSession ends one session and throws away the refresh tokens that could still be used on
// it. Tokens already used are left: they are what makes a second presentation recognisable.
func revokeSession(ctx context.Context, tx pgx.Tx, sessionID, reason string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE sessions SET revoked_at = now(), revoked_reason = $2
		WHERE id = $1 AND revoked_at IS NULL`, sessionID, reason); err != nil {
		return err
	}
	_, err := tx.Exec(ctx,
		`DELETE FROM refresh_tokens WHERE session_id = $1 AND used_at IS NULL`, sessionID)
	return err
}

// revokeMemberSessions is the same thing for every session of one member, inside a caller's
// transaction — removing a member and setting a new password both end sessions.
func revokeMemberSessions(ctx context.Context, tx pgx.Tx, memberID, reason string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE sessions SET revoked_at = now(), revoked_reason = $2
		WHERE member_id = $1 AND revoked_at IS NULL`, memberID, reason); err != nil {
		return err
	}
	return deleteUnusedTokens(ctx, tx, memberID)
}

// querier is the one method both a pool and a transaction have, so the two callers above can
// share the statement below.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func deleteUnusedTokens(ctx context.Context, q querier, memberID string) error {
	_, err := q.Exec(ctx, `
		DELETE FROM refresh_tokens t
		USING sessions s
		WHERE s.id = t.session_id AND s.member_id = $1 AND t.used_at IS NULL`, memberID)
	return err
}
