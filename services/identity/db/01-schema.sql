-- 01-schema.sql — the Identity Service's tables.
-- Runs automatically the first time the database container starts (see docker-compose.yml).

-- crypt() and gen_salt() hash the demo passwords in 02-seed.sql with bcrypt, the same algorithm
-- the service uses, so the seeded members can sign in once sign-in exists.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- One row per Member: a person given credentials to sign in, holding exactly one role.
CREATE TABLE IF NOT EXISTS members (
  id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  email               TEXT        NOT NULL CHECK (email = lower(email)),
  password_hash       TEXT        NOT NULL,                     -- bcrypt; the password itself is never stored
  temporary_password  BOOLEAN     NOT NULL DEFAULT true,        -- set by an Admin; must be replaced before signing in (FR-0.5)
  role                TEXT        NOT NULL CHECK (role IN ('admin', 'recruiter')),
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  removed_at          TIMESTAMPTZ                               -- set when removed; the row stays (FR-6.2)
);

-- An email belongs to at most one active member. A removed member's email is free again, so a
-- person who returns can be added as a new member.
CREATE UNIQUE INDEX IF NOT EXISTS members_active_email ON members (email) WHERE removed_at IS NULL;

-- One row per sign-in (ADR-009). A session is the chain of refresh tokens that starts there; it
-- ends at ends_at however often it is refreshed, or earlier when revoked.
CREATE TABLE IF NOT EXISTS sessions (
  id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  member_id      UUID        NOT NULL REFERENCES members (id),
  started_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  ends_at        TIMESTAMPTZ NOT NULL,                     -- the hard cap: eight hours from sign-in
  revoked_at     TIMESTAMPTZ,
  revoked_reason TEXT
);

CREATE INDEX IF NOT EXISTS sessions_member ON sessions (member_id) WHERE revoked_at IS NULL;

-- One row per refresh token ever issued in a session. The token itself is never stored: only
-- the SHA-256 of it, so a copy of this table holds nothing that can be presented.
--
-- A used row is kept until its session is swept away, because a second presentation of it is
-- how a stolen token is detected.
CREATE TABLE IF NOT EXISTS refresh_tokens (
  token_hash BYTEA       PRIMARY KEY,
  session_id UUID        NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
  issued_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  used_at    TIMESTAMPTZ                                   -- set when exchanged; a second use is reuse
);

CREATE INDEX IF NOT EXISTS refresh_tokens_session ON refresh_tokens (session_id);
