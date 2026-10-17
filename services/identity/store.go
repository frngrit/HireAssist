// store.go — everything that touches PostgreSQL: the Member type and one function per operation.
package main

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Member is one row of the members table, and the JSON the API returns for it. The password
// hash is deliberately not a field, so it can never end up in a response.
type Member struct {
	ID                string     `json:"id"`
	Email             string     `json:"email"`
	Role              string     `json:"role"`
	TemporaryPassword bool       `json:"temporary_password"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	RemovedAt         *time.Time `json:"removed_at"` // null while the member is active
}

// The ways an operation can fail that are the caller's fault rather than the database's.
var (
	ErrNotFound   = errors.New("member not found")
	ErrEmailTaken = errors.New("an active member already has this email")
	ErrRemoved    = errors.New("member has been removed")
	ErrLastAdmin  = errors.New("at least one active admin must remain")
)

// membershipLock is the key of a PostgreSQL advisory lock taken by every change that could leave
// the installation without an admin. Holding it makes such changes run one at a time, so two
// admins demoting each other at the same moment cannot both pass the last-admin check (FR-6.3).
const membershipLock = 6001

// memberColumns is the column list every query returns, in the order scanMember reads it.
const memberColumns = `id::text, email, role, temporary_password, created_at, updated_at, removed_at`

// Store runs the SQL.
type Store struct {
	db *pgxpool.Pool
}

func scanMember(row pgx.Row) (Member, error) {
	var m Member
	err := row.Scan(&m.ID, &m.Email, &m.Role, &m.TemporaryPassword, &m.CreatedAt, &m.UpdatedAt, &m.RemovedAt)
	return m, err
}

// Ping checks that the database answers; the readiness probe uses it.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.Ping(ctx)
}

// ListActive returns every member who has not been removed, oldest first (FR-6.5).
func (s *Store) ListActive(ctx context.Context) ([]Member, error) {
	rows, err := s.db.Query(ctx,
		`SELECT `+memberColumns+` FROM members WHERE removed_at IS NULL ORDER BY created_at, email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := []Member{} // an empty list is [] in JSON, not null
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// Get returns one member, removed or not, so a record of what a removed member did can still
// name them (FR-6.2).
func (s *Store) Get(ctx context.Context, id string) (Member, error) {
	m, err := scanMember(s.db.QueryRow(ctx, `SELECT `+memberColumns+` FROM members WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrNotFound
	}
	return m, err
}

// Create adds a member with a temporary password (FR-6.1).
func (s *Store) Create(ctx context.Context, email, passwordHash, role string) (Member, error) {
	m, err := scanMember(s.db.QueryRow(ctx, `
		INSERT INTO members (email, password_hash, role)
		VALUES ($1, $2, $3)
		RETURNING `+memberColumns,
		email, passwordHash, role))

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation on members_active_email
		return Member{}, ErrEmailTaken
	}
	return m, err
}

// ChangeRole sets a member's role, refusing to demote the last active admin (FR-6.1, FR-6.3).
func (s *Store) ChangeRole(ctx context.Context, id, role string) (Member, error) {
	var m Member
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		current, err := lockActiveMember(ctx, tx, id)
		if err != nil {
			return err
		}
		if current.Role == "admin" && role != "admin" {
			if err := ensureAnotherAdmin(ctx, tx, id); err != nil {
				return err
			}
		}
		m, err = scanMember(tx.QueryRow(ctx, `
			UPDATE members
			SET role = $2,
			    updated_at = CASE WHEN role = $2 THEN updated_at ELSE now() END
			WHERE id = $1
			RETURNING `+memberColumns,
			id, role))
		return err
	})
	return m, err
}

// SetPassword gives a member a new temporary password (FR-6.4) and ends every session they
// have: whoever is signed in as them got there with the password that was just replaced.
func (s *Store) SetPassword(ctx context.Context, id, passwordHash string) (Member, error) {
	var m Member
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := lockActiveMember(ctx, tx, id); err != nil {
			return err
		}
		var err error
		m, err = scanMember(tx.QueryRow(ctx, `
			UPDATE members
			SET password_hash = $2, temporary_password = true, updated_at = now()
			WHERE id = $1
			RETURNING `+memberColumns,
			id, passwordHash))
		if err != nil {
			return err
		}
		return revokeMemberSessions(ctx, tx, id, "password reset by an admin")
	})
	return m, err
}

// ReplaceOwnPassword is the member replacing their own password (FR-0.5): unlike SetPassword it
// clears the temporary flag, because the password is now one only the member knows. Their
// sessions end as well, since a password is usually changed because the old one may be known.
func (s *Store) ReplaceOwnPassword(ctx context.Context, id, passwordHash string) (Member, error) {
	var m Member
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := lockActiveMember(ctx, tx, id); err != nil {
			return err
		}
		var err error
		m, err = scanMember(tx.QueryRow(ctx, `
			UPDATE members
			SET password_hash = $2, temporary_password = false, updated_at = now()
			WHERE id = $1
			RETURNING `+memberColumns,
			id, passwordHash))
		if err != nil {
			return err
		}
		return revokeMemberSessions(ctx, tx, id, "password changed by the member")
	})
	return m, err
}

// Remove deactivates a member: they can no longer sign in, their sessions end, but the row
// stays so their past actions still name them (FR-6.2). The last active admin cannot be
// removed (FR-6.3).
func (s *Store) Remove(ctx context.Context, id string) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		current, err := lockActiveMember(ctx, tx, id)
		if err != nil {
			return err
		}
		if current.Role == "admin" {
			if err := ensureAnotherAdmin(ctx, tx, id); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx,
			`UPDATE members SET removed_at = now(), updated_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		// ADR-009: removing a member deletes their refresh tokens, which is what bounds their
		// remaining access to the lifetime of the access token they are holding.
		return revokeMemberSessions(ctx, tx, id, "member removed")
	})
}

// lockActiveMember takes the membership lock for the rest of the transaction, then returns the
// member, or ErrNotFound or ErrRemoved.
func lockActiveMember(ctx context.Context, tx pgx.Tx, id string) (Member, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, membershipLock); err != nil {
		return Member{}, err
	}
	m, err := scanMember(tx.QueryRow(ctx, `SELECT `+memberColumns+` FROM members WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrNotFound
	}
	if err != nil {
		return Member{}, err
	}
	if m.RemovedAt != nil {
		return Member{}, ErrRemoved
	}
	return m, nil
}

// ensureAnotherAdmin returns ErrLastAdmin unless an active admin other than id exists.
func ensureAnotherAdmin(ctx context.Context, tx pgx.Tx, id string) error {
	var others int
	err := tx.QueryRow(ctx, `
		SELECT count(*) FROM members
		WHERE role = 'admin' AND removed_at IS NULL AND id <> $1`, id).Scan(&others)
	if err != nil {
		return err
	}
	if others == 0 {
		return ErrLastAdmin
	}
	return nil
}
