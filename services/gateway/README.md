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
| Owner | the whole team — all four members jointly (ADR-005 rule 5) |

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
| `POST /auth/sign-out` | the gateway itself | nothing — clears the web app's refresh cookie |
| `GET /healthz`, `GET /readyz` | the gateway itself | nothing |

Anything else is a 404. Nothing is forwarded by default: a gateway that forwards the unknown is
a gateway that exposes the next service somebody adds.

## The web app's refresh cookie

ADR-010: ADR-009 asks the web app to keep the refresh token where scripts cannot
read it. The Identity Service returns it in the response body and leaves where it is kept to the
web app and the gateway; a script cannot set an HttpOnly cookie, so the gateway does
(`session_cookie.go`).

It is **opt-in per request**. A client that sends `X-Refresh-Token-Transport: cookie` on sign-in
or refresh gets the token as a cookie, `hireassist_refresh`, and a body without it. Every other
client (Postman, curl, the demo scripts) gets the Identity Service's answer untouched.

- The cookie is **HttpOnly**, **SameSite=Strict**, scoped to **`/auth`** and expires with the
  session's eight-hour cap. A proxy in front that adds a prefix rewrites the path to match; the
  web app's nginx and dev server turn it into `/api/auth`.
- On refresh, a body with no token is given the cookie's, and the cookie is not forwarded. No
  cookie at all is answered 401 here, without asking the Identity Service.
- A refused refresh clears the cookie, since its token is now worthless.
- `POST /auth/sign-out` clears it. The Identity Service has no sign-out, so the token is not
  revoked: it stays valid until the session's cap, but no browser holds it any more.
- It is **Secure** unless `SESSION_COOKIE_SECURE=false`, which only a plain-HTTP local run should
  set; the root compose file does.

Two tabs that refresh at the same moment present the same token twice, which the Identity
Service reads as a stolen copy and ends the session. In practice tabs load one after the other;
it is the price of single-use refresh tokens in a browser.

## How a token is checked

ADR-009: the Identity Service signs, everyone else verifies. The gateway holds only public
keys. It checks the ES256 signature and the expiry **itself** and never calls the Identity
Service to check a token — not per request, not at startup. The token is then forwarded
unchanged, so a service behind it can read who the caller is.

**The gateway is the only place a token is checked.** ADR-009 asks the services behind it to
verify what they receive as well; the team has decided against that, on the assumption that the
services are reachable only on the deployment's private network. That decision reverses part of
two accepted records and still needs an ADR of its own. What follows from it is operational:
nothing but the gateway may be published, because anything that can reach the Hiring or Identity
Service directly is already past every access check in the system.

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

Then, with the Hiring Service running on :8080 (`docker compose up -d hiring` at the repository
root; the full `docker compose up` runs a gateway of its own on :8000):

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
| `SESSION_COOKIE_SECURE` | `true` | `false` only for plain-HTTP local runs, where a browser may refuse a Secure cookie |

With no `JWT_PUBLIC_KEYS` the gateway starts, warns, serves `/healthz`, fails `/readyz`, and
answers 401 on every protected route. That is deliberate: a gateway that cannot verify is
running but not doing its job, and readiness should say so rather than let traffic arrive and
pile up as 401s.

Rate-limit buckets are held in memory, which suits one deployment per customer company
(ADR-006). Two replicas would each allow the full rate; moving the counter to a shared store is
a decision for when there is a second replica.
