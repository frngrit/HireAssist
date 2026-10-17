# HireAssist Web UI

The one web application for every signed-in role. It covers UC-0 (sign in), UC-1 (create a job
opening), UC-2 (batch screening) and UC-3 (interview guides). It talks only to the API Gateway
(ADR-001). It never reaches a service directly, and never the AI Service (ADR-004, ADR-007).

React 19 + TypeScript, built with Vite; Mantine for components; TanStack Query for server state;
MSW for the mock backend. English and Thai (NFR-16).

**Status:** every screen works against the real Gateway — sign-in (including replacing a
temporary password), job openings with AI-proposed criteria, batch screening and interview
guides — and against a **mock backend** that runs in the browser, so no service is needed to run,
demo or test it. Both follow the services' published contracts, see [Contracts](#contracts).

## Run it

Needs Node 22 or newer.

```sh
npm install
npm run dev          # http://localhost:5173 — mock mode, no backend needed
npm test             # the use cases end to end, against the same mocks
npm run build        # type-check and production build
```

Demo accounts (mock mode), the same synthetic members as the Identity Service's seed. The sign-in
page fills them in on click.

| Email | Role | Password |
|---|---|---|
| somchai@acme.example | recruiter | somchai-recruiter-password |
| nok@acme.example | admin | nok-admin-demo-password |

### Against the real services

`docker compose up -d --build` at the repository root builds this app against the gateway and
serves it on http://localhost:4173, with every service behind it. nginx does what the dev
server's proxy does (below). To work on the app against that running stack instead:

```sh
VITE_GATEWAY_URL=http://localhost:8000 npm run dev:live
```

`dev:live` turns the mocks off. Every call goes to `/api/...`, and the dev server proxies it to
the Gateway **with the `/api` prefix removed**, so the Gateway's routes stay `/auth/*` and
`/job-openings/*`.

The prefix exists because the app's page routes and the API's paths would otherwise collide: a
reload of the page `/job-openings/jo_123` would be sent to the Hiring Service instead of the app.
Whatever serves the built app must strip `/api` the same way; `nginx.conf` does it in the container.

## Mock mode

`src/mocks/` is an in-memory stand-in for the Gateway and the services behind it. The same
handlers serve the browser demo (through a service worker) and the tests (through MSW's Node
server), so a passing test means the demo behaves the same way.

It behaves like the real system where the demo needs it:

- **Sessions** behave like the gateway's: the refresh token is a simulated HttpOnly cookie that
  survives a reload, used once and rotated on every refresh. An expired access token is renewed
  and the request retried, invisibly (see [Sessions](#sessions)).
- **Batches** are accepted at once (FR-2.2) and then processed resume by resume over a few
  seconds. The ranking updates while they finish (FR-2.6), and a notification says when a batch
  is done (FR-2.12).
- **A file whose name contains "scan"** ends in *needs manual review* and is never ranked, like
  the scanned PDFs the real pipeline cannot read (FR-2.7, ADR-008).
- **Scores are deterministic per file name**, so a demo recording gives the same result every time.
  The seed includes one finished batch, a candidate who misses a must-have, and one shortlisted
  candidate, so every state can be shown without setup.
- The criteria proposal and the interview questions are simple templates, not a model. They show
  the shapes, not the quality.
- **Files that are not PDFs** are left out of a batch and listed, as the Hiring Service does.

Everything in the mock data is synthetic. Never put a real resume in the demo (PDPA).

## Sessions

ADR-009 asks for the refresh token to be kept where scripts cannot read it, and ADR-010 has the
gateway do it. The app asks the gateway for that with the `X-Refresh-Token-Transport: cookie` header on sign-in and refresh: the
gateway moves the token into an HttpOnly cookie, and the app's code never sees it. The access
token lives in memory only, so a reload or a new tab gets a fresh one with the cookie. How the
gateway does it is in its README.

The gateway scopes the cookie to `/auth`, and both proxies in front of it rewrite that to
`/api/auth`: the dev server with `cookiePathRewrite`, the container's nginx with
`proxy_cookie_path`.

Signing out calls the gateway's `POST /auth/sign-out`, which clears the cookie. The Identity
Service has no sign-out, so the refresh token stays valid until the session's eight-hour cap,
but no browser holds it any more.

A member whose password an Admin set is answered 403 at sign-in (FR-0.5). The sign-in page then
asks for a new password, calls `POST /auth/change-password`, and signs in with it.

## Contracts

Every shape in `src/api/types.ts` is copied from the service that owns it.

| Endpoint (Gateway path) | Operation | Source |
|---|---|---|
| `POST /auth/sign-in` · `POST /auth/refresh` · `POST /auth/change-password` | `Identity.authenticateMember()`, `changePassword()` | The Identity Service, with the refresh token as the gateway's cookie |
| `POST /auth/sign-out` | the gateway | Clears the refresh cookie |
| `GET/POST /job-openings` · `GET /job-openings/{id}` · `PUT …/criteria` · `POST …/pause` · `…/resume` · `…/close` | Hiring, UC-1 | The Hiring Service's OpenAPI document |
| `POST /job-openings/propose` | `proposeJobOpening()` | 〃 — 502 when the AI Service is down, and the app offers manual entry (FR-1.5) |
| `POST /job-openings/{id}/batches` (multipart, field `resumes`) · `GET …/batches/{batchId}` | `submitScreeningBatch()`, `getBatchStatus()` | 〃 |
| `GET /job-openings/{id}/results?min_score=&criterion=` | `getRankedResults()` | 〃 |
| `GET /job-openings/{id}/results/{resumeId}` · `POST …/override` · `POST …/shortlist` | `getScreeningResult()`, `overrideScore()`, `shortlistCandidate()` | 〃 |
| `GET/POST /job-openings/{id}/candidates/{cid}/interview-guide` · `POST …/regenerate` · `PUT …/questions` · `PUT …/questions/{qid}/note` | `getInterviewGuide()`, `generateInterviewGuide()`, `reviseGuide()`, `recordInterviewNote()` | 〃 — needs Resume Processing and the AI Service behind Hiring; without them it answers 502 |

How the app works within that contract:

- **There is no operation that lists an opening's batches**, so the batch panel shows the batches
  the ranked results mention plus the ones uploaded from this browser (kept in `localStorage`),
  each fetched by id. A batch uploaded from another browser appears once its first resume has a
  result. A `GET /job-openings/{id}/batches` in the Hiring Service would remove the workaround.
- **An override is a score only.** The contract keeps the original score beside it (FR-2.8) but
  takes no reason, so the app asks for none.
- **The criterion filter offers must-haves only**: screening reports which must-haves a resume
  missed, and judges nice-to-haves only through the score.
- **Screening is keyed by resume, interview guides by candidate.** The drawer opens a resume
  (`?resume=`), and the guide is reached through the candidate id the result carries.
- **The written justification may be missing.** Hiring reads it from Resume Processing when a
  resume is opened, and shows the record without it when that service does not answer.
- **Shortlisting gates the interview guide** in the app and the mock, as UC-3's precondition says.
  The Hiring Service does not check it yet.
- **Candidates are shown by file name.** Nothing the app receives carries a name or contact
  details, so the resume's file name is the only label.

When a service changes its contract, change `types.ts`, the matching handler in
`src/mocks/handlers.ts`, and the screen. The type checker will point at the screen.

## Layout

```
src/
  api/          types.ts (the contracts), client.ts (fetch, tokens, refresh), endpoints.ts (one function per operation)
  auth/         session state; restores the session on load (ADR-009)
  i18n/         en.ts is the reference; th.ts must define every key, or the build fails
  mocks/        db.ts (seed + simulated pipeline), handlers.ts (the fake Gateway), browser.ts
  pages/        one per route: sign-in, openings list, new opening, opening detail, interview guide
  components/   layout, criteria editor, screening tab (upload, batches, ranked list, candidate drawer)
  test/         use case tests: auth, uc1, uc2, uc3, i18n
```

Tests run in happy-dom. jsdom cannot send a multipart upload through Node's `fetch` in this
Vitest version: the request body never finishes reading.
