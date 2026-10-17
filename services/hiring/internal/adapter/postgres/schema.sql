-- Hiring Service schema (ADR-003: PostgreSQL is the system of record). Applied on every start,
-- so every statement must be idempotent.

CREATE TABLE IF NOT EXISTS job_openings (
    id                   text        PRIMARY KEY,
    -- Insertion order, so List returns openings in the order they were created.
    seq                  bigint      GENERATED ALWAYS AS IDENTITY UNIQUE,
    title                text        NOT NULL CHECK (btrim(title) <> ''),
    description          text        NOT NULL,
    expected_time_to_fill interval   NOT NULL CHECK (expected_time_to_fill > interval '0'),
    status               text        NOT NULL CHECK (status IN ('open', 'paused', 'closed')),
    opened_at            timestamptz NOT NULL,
    -- Set exactly when the opening is closed (FR-1.8).
    outcome              text        CHECK (outcome IN ('filled', 'cancelled')),
    CHECK ((status = 'closed') = (outcome IS NOT NULL))
);

CREATE TABLE IF NOT EXISTS job_opening_criteria (
    job_opening_id text    NOT NULL REFERENCES job_openings (id) ON DELETE CASCADE,
    position       integer NOT NULL,
    name           text    NOT NULL CHECK (btrim(name) <> ''),
    classification text    NOT NULL CHECK (classification IN ('must-have', 'nice-to-have')),
    weight         integer NOT NULL CHECK (weight > 0),
    PRIMARY KEY (job_opening_id, position)
);

-- UC-2. A screening batch, and the snapshot of the criteria it is judged against (NFR-15):
-- revising the opening's criteria later does not change what a submitted resume is judged by.
CREATE TABLE IF NOT EXISTS screening_batches (
    id             text        PRIMARY KEY,
    seq            bigint      GENERATED ALWAYS AS IDENTITY UNIQUE,
    job_opening_id text        NOT NULL REFERENCES job_openings (id) ON DELETE CASCADE,
    submitted_at   timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS screening_batch_criteria (
    batch_id       text    NOT NULL REFERENCES screening_batches (id) ON DELETE CASCADE,
    position       integer NOT NULL,
    name           text    NOT NULL CHECK (btrim(name) <> ''),
    classification text    NOT NULL CHECK (classification IN ('must-have', 'nice-to-have')),
    weight         integer NOT NULL CHECK (weight > 0),
    PRIMARY KEY (batch_id, position)
);

-- One resume of a batch. requested_at doubles as the outbox: it stays NULL until the broker has
-- confirmed the request to screen the resume, so a request a broker outage held back is found
-- and sent again rather than lost (NFR-10).
CREATE TABLE IF NOT EXISTS screening_entries (
    resume_id        text        PRIMARY KEY,
    batch_id         text        NOT NULL REFERENCES screening_batches (id) ON DELETE CASCADE,
    position         integer     NOT NULL,
    file_name        text        NOT NULL,
    object_key       text        NOT NULL,
    size_bytes       bigint      NOT NULL CHECK (size_bytes >= 0),
    status           text        NOT NULL CHECK (status IN ('pending', 'screened', 'needs_review', 'failed')),
    requested_at     timestamptz,
    candidate_id     text,
    score            integer     CHECK (score BETWEEN 0 AND 100),
    meets_must_haves boolean,
    -- Positions, in the batch's snapshot, of the must-haves judged unmet (FR-2.5).
    unmet_must_haves integer[]   NOT NULL DEFAULT '{}',
    reason           text,
    completed_at     timestamptz,
    override_score   integer     CHECK (override_score BETWEEN 0 AND 100),
    overridden_at    timestamptz,
    shortlisted_at   timestamptz,
    UNIQUE (batch_id, position),
    -- Scored exactly when screened: an unparsable resume is never given a score (FR-2.7).
    CHECK ((status = 'screened') = (score IS NOT NULL)),
    CHECK ((override_score IS NULL) = (overridden_at IS NULL))
);

CREATE INDEX IF NOT EXISTS screening_entries_unrequested ON screening_entries (batch_id, position)
    WHERE status = 'pending' AND requested_at IS NULL;
