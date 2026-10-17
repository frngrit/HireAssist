# API Gateway

The single entry point for user traffic (ADR-001). It owns no domain data. For every request it
matches a route, verifies the access token, checks the role the route requires, rate-limits the
caller, and forwards the request to the service that owns it.

**Status:** routing, token verification, the role gate and rate limiting work, and the Identity
Service now signs tokens, so signing in through the gateway works end to end. One thing is still
waiting on other work: the Compliance & Insights Service does not exist, so a rejected request is
logged rather than sent to the access audit log (FR-0.4).

TLS termination (ADR-001) is not here either — it belongs in front of this process in a real
deployment, and locally everything is plain HTTP.

| | |
|---|---|
| API | http://localhost:8000 |
| Owner | *unassigned* (ADR-005 rule 5) |

## Routes

The whole table is in `route.go`. Paths are forwarded unchanged, so each one can be read
against the owning service's OpenAPI document without translating it.

| Path | Goes to | Needs |
|---|---|---|
| `POST /auth/sign-in` | Identity | nothing — a caller cannot hold a token before signing in |
| `POST /auth/refresh` | Identity | nothing — an expired access token must still be renewable |
| `POST /auth/change-password` | Identity | nothing — a member with a temporary password cannot sign in first (FR-0.5); the Identity Service asks for the current password |
| `/members/*` | Identity | Admin |
| `/job-openings/*` | Hiring | either role |
| `GET /healthz`, `GET /readyz` | the gateway itself | nothing |

Anything else is a 404. Nothing is forwarded by default: a gateway that forwards the unknown is
a gateway that exposes the next service somebody adds.

## How a token is checked

ADR-009: the Identity Service signs, everyone else verifies. The gateway holds only public
keys. It checks the ES256 signature and the expiry **itself** and never calls the Identity
Service to check a token — not per request, not at startup. The token is then forwarded
unchanged, so a service behind it can read who the caller is.

**The gateway is the only place a token is checked (ADR-010).** ADR-009 asked the services
behind it to verify what they receive as well; ADR-010 withdraws that, on the assumption that the
services are reachable only on the deployment's private network. What follows from it is
operational: nothing but the gateway may be published, because anything that can reach the Hiring
or Identity Service directly is already past every access check in the system.

Only ES256 is accepted. A token that names its own algorithm is how a verifier gets talked into
accepting `none`, and there is a test for exactly that.

More than one key may be configured, which is how a key is rotated: add the new one, accept
both while old tokens are still alive, remove the old one. While two are configured a token
must name the key it was signed with. With a single key handed over as a bare PEM block there
is no rotation to get wrong, so the signature alone decides.

## Run it

Needs Docker. No Go toolchain is assumed — `make` runs the compiler in a container.

```sh
make keys      # a development key pair; prints the export line for the public half
export JWT_PUBLIC_KEYS="..."   # as printed
make run
```

Then, with the Hiring Service running on :8080 (`docker compose up` at the repository root):

```sh
curl -i localhost:8000/job-openings                      # 401, no token
curl -i localhost:8000/healthz                           # 200
curl -i -H "Authorization: Bearer $TOKEN" localhost:8000/job-openings
```

`$TOKEN` comes from signing in. The Identity Service's `make keys` writes the same development
key pair and prints the `JWT_PUBLIC_KEYS` line to export here:

```sh
curl -s localhost:8000/auth/sign-in \
  -H 'Content-Type: application/json' \
  -d '{"email":"nok@acme.example","password":"nok-admin-demo-password"}'
```

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `PORT` | `8000` | |
| `IDENTITY_URL` | `http://localhost:8081` | |
| `HIRING_URL` | `http://localhost:8080` | |
| `JWT_PUBLIC_KEYS` | — | one PEM block, or a JSON array of `{"kid", "pem"}` |
| `RATE_LIMIT_PER_SECOND` | `20` | per caller |
| `RATE_LIMIT_BURST` | `40` | how many may arrive at once |

With no `JWT_PUBLIC_KEYS` the gateway starts, warns, serves `/healthz`, fails `/readyz`, and
answers 401 on every protected route. That is deliberate: a gateway that cannot verify is
running but not doing its job, and readiness should say so rather than let traffic arrive and
pile up as 401s.

Rate-limit buckets are held in memory, which suits one deployment per customer company
(ADR-006). Two replicas would each allow the full rate; moving the counter to a shared store is
a decision for when there is a second replica.
