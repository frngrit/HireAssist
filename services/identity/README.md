# Identity Service

Owns the **members** of one HireAssist installation and their roles: UC-0 (sign in) and UC-6
(manage members and roles). Go with the standard library's `net/http`, PostgreSQL through `pgx`.

**Status:** member CRUD and signing in work. Signing in issues the tokens ADR-009 describes, and
the gateway verifies them. What is still missing is the other half of that record: **these
endpoints are themselves unprotected** — the service does not yet verify the token on a request
to `/members`, so run it behind the gateway, or only on your own machine.

| | |
|---|---|
| API | http://localhost:8081 |
| Database | PostgreSQL 16 in Docker, `localhost:5433`, database `identity` |
| Owner | Thanwarat Korcharoenkiat |

## Run it

Needs Docker Desktop running and Go 1.27 or newer. `jq` is needed only for `make demo`.

```sh
make db-up      # start PostgreSQL; the first start creates the tables and 2 demo members
make keys       # the development signing key pair, and the line to export for the gateway
make run        # start the service — leave this terminal open
```

`make run` makes the key pair if it is not there and passes the private half in. A database
created before sessions existed has no `sessions` table: `make db-schema` adds it, or
`docker compose down -v && make db-up` starts over.

In a second terminal:

```sh
make db-watch   # the members table, refreshed every 2 seconds (Ctrl+C stops)
make db-reset   # back to the 2 demo members — run before each recording
make demo       # every call in order with curl, Enter between steps
```

`make` alone lists every command. `make db-down` stops the database and keeps the data;
`docker compose down -v` wipes it.

| Variable | Default | Meaning |
|---|---|---|
| `PORT` | `8081` | |
| `DATABASE_URL` | — | no default: a connection string carries a password |
| `JWT_PRIVATE_KEY` | — | the ES256 signing key, in PEM. Required; `make run` passes the development one |
| `JWT_KEY_ID` | — | the `kid` put in each token, so a verifier holding two keys knows which signed it. `make run` uses `dev-1` |
| `ACCESS_TOKEN_TTL` | `15m` | how long an access token is good for |
| `SESSION_LIFETIME` | `8h` | the cap on a session, however often it is refreshed |

## Endpoints

| Operation | Method and path | Success | Errors |
|---|---|---|---|
| `authenticateMember()` | `POST /auth/sign-in` | 200, a token pair | 401 wrong email or password · 403 the temporary password must be replaced |
| refresh | `POST /auth/refresh` | 200, the next token pair | 401 unknown, used, revoked or expired |
| `changePassword()` | `POST /auth/change-password` | 200, the member | 400 · 401 wrong current password |
| `listMembers()` | `GET /members` | 200, active members only | |
| `getMember()` | `GET /members/{id}` | 200, removed members too | 400 id not a UUID · 404 |
| `createMember()` | `POST /members` | 201 and `Location` | 400 · 409 email already active |
| `changeMemberRole()` | `PATCH /members/{id}` | 200 | 400 · 404 · 409 removed or last admin |
| `setMemberPassword()` | `PUT /members/{id}/password` | 200 | 400 · 404 · 409 removed |
| `removeMember()` | `DELETE /members/{id}` | 204, no body | 400 · 404 · 409 removed or last admin |
| liveness | `GET /healthz` | 200 while the process runs | |
| readiness | `GET /readyz` | 200 while the database answers | 503 |

Every error is JSON, `{"error": "...", "details": [...]}`. `details` lists every validation
problem at once.

```json
{
  "id": "143ae1e8-9733-4d17-9ced-5075459ab63b",
  "email": "ploy@acme.example",
  "role": "recruiter",
  "temporary_password": true,
  "created_at": "2026-10-02T18:47:54.659135+07:00",
  "updated_at": "2026-10-02T18:47:54.659135+07:00",
  "removed_at": null
}
```

`POST /members` takes `email`, `password` and `role`. `PATCH /members/{id}` takes `role` only.
`PUT /members/{id}/password` takes `password`.

## Signing in (UC-0, ADR-009)

```sh
curl -s localhost:8081/auth/sign-in -H 'Content-Type: application/json' \
  -d '{"email":"nok@acme.example","password":"nok-admin-demo-password"}'
```

```json
{
  "access_token": "eyJhbGciOiJFUzI1NiIsImtpZCI6ImRldi0xIiwidHlwIjoiSldUIn0…",
  "token_type": "Bearer",
  "expires_in": 900,
  "refresh_token": "o3mQ…",
  "session_expires_at": "2026-10-17T20:14:03Z",
  "member": { "id": "00000000-0000-4000-8000-000000000001", "role": "admin", … }
}
```

**The access token** is an ES256 JWT carrying `sub` (the member id), `role` (`Admin` or
`Recruiter`, as the requirements name them — the database spells them lower-case, and the token
is where the two meet), `iat` and `exp`. It lasts fifteen minutes. Nobody calls this service to
check one: the gateway and any service acting on the role hold the public key and verify it
themselves.

**The refresh token** is 256 random bits with no meaning of their own. Only its SHA-256 is
stored, so a copy of the table yields nothing that can be presented. `POST /auth/refresh`
exchanges it for a new pair and retires the one presented — a retired token presented again
means a copy exists, so the whole session is revoked. The session ends eight hours after
sign-in however often it is refreshed.

The refresh token comes back in the body rather than a cookie: where it is kept is a decision
for the web application and the gateway (ADR-009), and setting a cookie here would make it for
them.

**A temporary password cannot open a session** (FR-0.5). Sign-in answers 403 and the member
replaces it first:

```sh
curl -s localhost:8081/auth/change-password -H 'Content-Type: application/json' \
  -d '{"email":"ploy@acme.example","current_password":"…","new_password":"…"}'
```

That endpoint takes credentials rather than a token, because a member in this state cannot sign
in to get one. It clears the temporary flag and ends every session that member has.

## Rules

| Rule | Why |
|---|---|
| An email is trimmed and lower-cased, and belongs to at most one active member | `Ploy@Acme.example` and `ploy@acme.example` are the same person |
| A password is at least 15 characters and at most 72 bytes; no forced capitals, digits or symbols | NIST's current guidance for a password that is the only factor; bcrypt reads only 72 bytes |
| A password an Admin sets is temporary | the member must replace it before signing in (FR-0.5) |
| Setting, changing or removing ends that member's sessions | ADR-009 bounds a removed member's access to the lifetime of the access token they hold; their refresh tokens are deleted |
| Member management re-reads the role from the database | a demoted Admin loses that power immediately rather than when their token expires (ADR-009). Not yet enforced here — these endpoints do not read the token at all |
| Removing a member keeps the row, marked removed | past actions must still name the person (FR-6.2) |
| The last active admin cannot be removed or demoted | nothing in the product could create an admin again (FR-6.3); changes that could do it run one at a time, so two admins demoting each other cannot both succeed |
| Only `role` can be changed with PATCH; email is fixed | FR-6.1 |

## Demo members

Synthetic, from `db/02-seed.sql`, with fixed ids so every recording starts the same.

| Email | Role | Password | id |
|---|---|---|---|
| nok@acme.example | admin | nok-admin-demo-password | `00000000-0000-4000-8000-000000000001` |
| somchai@acme.example | recruiter | somchai-recruiter-password | `00000000-0000-4000-8000-000000000002` |

## Recording the CRUD demo (under 5 minutes)

**Before pressing record:** `make db-up`, then `make run` in one terminal, then `make db-reset`
and `make db-watch` in a second. Import `HireAssist-Identity.postman_collection.json` into
Postman (File → Import). Put Postman in the top two-thirds of the screen and the `db-watch`
terminal below it, so the table refreshes by itself after every call.

| Time | Send | Point out |
|---|---|---|
| 0:00 | — | the `make run` terminal: the service is up on 8081 and connected to PostgreSQL |
| 0:20 | 01 READ ALL | 200, the two demo members, the same rows as the table below |
| 0:45 | 02 CREATE | 201, the email stored lower-case, `temporary_password` true, a new row in the table |
| 1:20 | 03 and 04 | 400 lists every problem at once; 409 for an email that already belongs to someone |
| 1:50 | 05 READ ONE | 200, one member by id |
| 2:10 | 06 UPDATE role | 200, the role in the table turns to admin and `updated` moves |
| 2:40 | 07 UPDATE password | 200, the password is temporary again |
| 3:00 | 08 DELETE | 204; the row stays, with `removed` filled in |
| 3:20 | 09 and 10 | the removed member can still be read by id, but is gone from the list |
| 3:50 | 11 DELETE last admin | 409, the installation must keep an admin |
| 4:15 | — | the `make run` terminal: one JSON log line per request, with its status |

Each Postman request carries tests, so the Test Results tab turns green when the status is right.

## Files

| File | What it is |
|---|---|
| `main.go` | Configuration, the database connection, the routes, and a graceful shutdown |
| `members.go` | The six member handlers, the readiness probe, and the error-to-status mapping |
| `token.go` | The access token and the refresh token: what they hold, and how they are minted |
| `auth.go` | Signing in, refreshing, and replacing a temporary password |
| `sessions.go` | The sessions and refresh tokens half of the store, including reuse detection |
| `token_test.go` | What a token must contain for another service to accept it |
| `store.go` | The `Member` type and one SQL function per operation |
| `validate.go` | The rules for emails, passwords, roles and ids, and password hashing |
| `http.go` | JSON in and out, error bodies, and the request log |
| `db/01-schema.sql` | The `members`, `sessions` and `refresh_tokens` tables; runs on the first database start |
| `db/02-seed.sql` | The two demo members; also what `make db-reset` runs |
| `docker-compose.yml` | The PostgreSQL container |
| `Makefile` | The commands above |
| `demo.sh` | The walkthrough behind `make demo` |
| `HireAssist-Identity.postman_collection.json` | The same calls for Postman, in demo order, each with tests |

## If something does not start

- `DATABASE_URL is not set` — start the service with `make run`, which passes it.
- `JWT_PRIVATE_KEY is not set` — `make keys`, or use `make run`, which does it. A service that
  cannot sign cannot authenticate anyone, so it refuses to start rather than fail the first
  sign-in.
- `relation "sessions" does not exist` — the database predates sessions; `make db-schema`.
- `cannot reach PostgreSQL` — Docker Desktop is not running, or `make db-up` was not run.
- `address already in use` on 8081 — another copy is running; `PORT=8082 make run` moves it,
  and the `base` variable in Postman must move with it.
- The table is missing after `make db-up` — the init scripts only run on a fresh volume:
  `docker compose down -v && make db-up`.
