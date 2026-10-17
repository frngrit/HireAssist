# ADR-012: The gateway keeps the web application's refresh token in an HttpOnly cookie

**Date:** 2026-10-17
**Deciders:** the whole team, who jointly own the gateway; written by Rerngrit Jangsri (web
application owner)

> In the context of a browser holding a signed-in member's session, facing a refresh token that
> must survive a page reload yet stay out of reach of the page's own scripts, we decided that the
> API gateway move the refresh token out of the Identity Service's answer and into an HttpOnly
> cookie, for clients that ask for it, to achieve a session that survives a reload without any
> script ever reading the long-lived credential, accepting that the gateway now reads and rewrites
> two kinds of exchange instead of passing every request through untouched.

---

## Summary

### Issue

A member signs in with an email address and a password and receives a token that identifies them
and their role (FR-0.1). ADR-009 made that two tokens: a JWT access token that expires after
fifteen minutes, and a refresh token that is exchanged once for the next pair and retired. A
session is the chain of refresh tokens that starts at one sign-in, and it ends eight hours later.

ADR-009 left one question open, and named who answers it: the web application must keep the
refresh token where scripts cannot read it, and that is for the web application and the gateway
to decide. The Identity Service returns the refresh token in the body of its response and sets no
cookie, on purpose, so as not to make that decision for them.

Three forces meet here.

**The refresh token is the session.** Whoever holds it can keep a member signed in for the rest
of the eight hours. A script injected into the page — through a compromised dependency or a
cross-site scripting bug — that can read the token can carry it away.

**A page reload discards everything a script holds in memory.** If the refresh token lives only
there, every reload, every new tab and every link opened from an email signs the member out.

**A script cannot create an HttpOnly cookie.** The one place in a browser that scripts cannot
read is a cookie that a server sets with the HttpOnly attribute. Only something on the response
path can set it, which in this architecture means the Identity Service or the gateway in front of
it.

### Decision

**The gateway stores the web application's refresh token in an HttpOnly cookie, on request.**

- A client that sends the header `X-Refresh-Token-Transport: cookie` on sign-in or refresh gets
  the refresh token as a cookie, and a response body without it. Every other client gets the
  Identity Service's response unchanged.
- The cookie is **HttpOnly**, **SameSite=Strict**, **Secure**, scoped to the `/auth` path and
  expires when the session does.
- On refresh, the gateway puts the cookie's token into the request body before passing it to the
  Identity Service, and does not pass the cookie on. With no cookie and no token in the body, it
  answers 401 itself.
- A refused refresh clears the cookie.
- The gateway serves `POST /auth/sign-out` itself, which clears the cookie.
- The access token stays in the page's memory. After a reload the page gets a new one by
  refreshing with the cookie.

### Status

**Accepted.** The gateway is owned by the whole team, which agreed to it.

### Group

Security

---

## Details

### Assumptions

- The web application and the gateway are served from one site, with a proxy in front stripping
  a path prefix the application uses for API calls. A cookie with SameSite=Strict is sent only to
  that one site.
- A deployment serves HTTPS. Only a local run on plain HTTP turns the Secure attribute off.
- Postman collections, demo scripts and other non-browser clients send no such header and keep
  the refresh token in the body, as the Identity Service designed.

### Constraints

- ADR-009 fixes the tokens, their lifetimes, single-use rotation and reuse detection. This
  record changes none of that, only where a browser keeps the refresh token.
- The Identity Service returns the refresh token in the body and has no sign-out operation.
- The services behind the gateway are reachable only through it, so the gateway sees every
  sign-in and refresh.

### Positions

1. **Keep both tokens in the page's memory.**
2. **Browser storage** — `localStorage` or `sessionStorage`.
3. **The Identity Service sets the cookie itself.**
4. **The gateway moves the token into an HttpOnly cookie, on request** *(chosen)*.
5. **The gateway always uses the cookie**, for every client.
6. **A backend-for-frontend**: a server beside the web application that keeps the refresh token
   on the server and gives the browser a session cookie of its own.

### Argument

**Against memory only (1).** It is the safest place a script can use, and it needs no help from
a server. But every reload, new tab or link from an email signs the member out. That is not
usable for a tool a recruiter keeps open all day, and it is not what ADR-009 asks for.

**Against browser storage (2).** It survives a reload, and it is a few lines of code. It is also
exactly the place ADR-009 rules out: any script on the page can read it, so one injected script
takes the whole session.

**Against the Identity Service setting it (3).** It works, but the Identity Service would then
have to know the browser's path layout and cookie rules and treat browser clients differently
from others. Its owner deliberately left that out of the service, and the gateway already sits
between the browser and every service.

**Against always using the cookie (5).** Postman collections, the Identity Service's demo
script and anything else that is not a browser would lose the refresh token they read from the
body. Asking for the cookie is one header for the web application; making it the default breaks
every other client for no gain.

**Against a backend-for-frontend (6).** It is the strongest design — no token ever reaches the
browser — and it is a second server beside the gateway, holding sessions of its own. That is the
reason a server-rendering framework was turned down for the web application.

**For the chosen option (4).** No script ever holds the refresh token, the session survives a
reload and opens in a new tab, every other client is unaffected, and it takes about a hundred
lines in a component that already sees every sign-in and refresh.

### Implications

**What this buys us**

- The refresh token never reaches the page's scripts. An injected script can use the access token
  for at most fifteen minutes, and cannot extend the session.
- A member stays signed in across reloads and tabs until the session's eight-hour cap.
- SameSite=Strict keeps another site from making the browser spend the cookie, and the `/auth`
  path means it is sent nowhere else.
- Clients that are not browsers see the Identity Service's contract unchanged.

**What it costs us**

- **The gateway is no longer a pure pass-through.** It reads and rewrites the body of two kinds of
  exchange and serves a route of its own. It is the only place it does either.
- **Signing out does not revoke the session.** The cookie is cleared, but the Identity Service
  has no sign-out, so the token stays valid until the session's cap. Nothing in a browser holds
  it any more.
- **Two tabs refreshing at the same moment end the session.** Both present the same single-use
  token, which the Identity Service reads as a stolen copy and revokes. Tabs normally load one
  after another.
- **The cookie's path depends on the proxy in front.** The gateway scopes it to `/auth`, and a
  proxy that adds a prefix must rewrite the path to match, in development as in deployment.
- **The web application and the gateway must share a site.** Serving them from different sites
  would need the SameSite rule relaxed, and cross-site request protection added in its place.

---

## Related

### Related decisions

- [ADR-009](ADR-009-jwt-with-refresh-tokens.md) — the tokens and their rules. This record answers
  the question it left to the web application and the gateway: where the refresh token is kept.
- [ADR-001](ADR-001-service-decomposition.md) — the gateway as the single entry point for user
  traffic, which is why it sees every sign-in and refresh.
- [ADR-006](ADR-006-single-tenant-deployment.md) — one deployment per company, so the web
  application and the gateway are served from one site.

### Related requirements

- **UC-0, signing in**, is the use case served.
- **FR-0.1**, signing in and getting a token that identifies the member and their role, is
  unchanged. Only where the browser keeps the refresh token changes.
- **FR-0.2**, rejecting a missing, invalid or expired token, now includes a refresh with no
  cookie, which the gateway rejects itself.
- **NFR-11**, sending all data over TLS, is what the cookie's Secure attribute depends on.
- **NFR-16**, a web interface in current browsers, is where the session has to survive a reload.
- **Course requirements:** authentication alongside the business use cases, and an API gateway.

### Related principles

- *PDPA is a design force, not a feature* — the credential that opens the candidate data for
  eight hours is kept out of reach of anything running in the page.
- *Easily reversible* — the cookie is opt-in per request. Removing it changes the gateway and the
  web application, and nothing in the Identity Service.
