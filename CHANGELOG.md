# Changelog

A running summary of what changed in this project's documentation and why.

One entry per working session, newest first, headed `YYYY-MM-DD HH:MM`. Entries record
**decisions and their reasons**, not file diffs — Git already has the diffs.

**Keep entries short.** A line or two per change: what changed, and why in a clause. If an
explanation needs a paragraph, it belongs in the ADR or the document itself, and the entry should
point there instead of repeating it. A changelog nobody scrolls through is a changelog nobody
reads.

Each entry is written once and then left alone. Later work gets a new entry — an entry describes
what a session decided, and rewriting it destroys the record of when and why something changed.

---

## 2026-10-17 21:45 — Two records written for decisions implementation had already made

- **ADR-010: the gateway is the only place an access token is verified.** *Proposed* — it binds
  the Identity and Hiring Services, whose owners have not agreed. It states what it overrides:
  ADR-009's requirement that every service acting on the role verify the token itself, and
  ADR-001's position that enforcement cannot live at the gateway alone. Its argument is that the
  check would otherwise be written once per language for no protection against the internal
  calls that carry no member token at all, and that one deployment serves one company. Its
  costs are stated plainly, including the two that are easy to miss: a demoted Admin is now
  stopped only when their token expires, and the deployment rule *publish nothing but the
  gateway* becomes the protection for candidate data rather than an operational preference.
- **ADR-011: interview guides live in a document store belonging to the Hiring Service.**
  *Accepted* — ADR-003 had already decided the store class, and whose database is within one
  service's own storage. The nearly-chosen alternative, a JSON column in the hiring record, is
  recorded with the two reasons it lost: erasure stops being *drop the documents, keep the
  counts*, and the storage rule stops being predictable for the next AI-derived artifact.
- The index gained both rows, the living view now shows what ADR-010 changed in ADR-001, ADR-005
  and ADR-009 and what ADR-011 settled in ADR-003, and the session-and-token candidate decision
  is removed as answered. No earlier record was edited.
- Outstanding: an owner for the gateway (ADR-005 rule 5); UC-3's shortlisted precondition,
  which can now be checked since shortlisting exists; the proposal's ADR index, which still
  stops at ADR-006.

---

## 2026-10-17 19:39 — Screening built to the Resume Processing contract

- **UC-2 runs end to end through RabbitMQ**, on the terms of `resume-processing/MESSAGES.md`,
  against the real Resume Processing Service. Hiring keeps each request in PostgreSQL until the
  broker confirms it, so a broker outage delays screening and loses nothing (NFR-10). A result it
  can never accept goes to its own `screening.completed.dlq`, added to the contract as a
  Hiring-internal queue: the shared queues take no dead-letter arguments.
- **Hiring keeps the score, the must-have check and the unmet must-haves, not the
  justification**, though the message carries it — the table's *Hiring reads the profile, it does
  not copy it*, and ADR-003's boundary. One consequence: filtering the ranking by criterion works
  on must-haves only, since nice-to-haves come back only through the score.
- **The ranking puts candidates who meet every must-have first**, then orders by score: a
  must-have is a gate, not a weight (FR-2.5).
- **`getScreeningResult()` added to the SOC table** — opening one screened resume, which reads the
  justification from Resume Processing.
- **Shortlisting exists now**, so UC-3's precondition can be checked; generating a guide does not
  check it yet.
- **A service Hiring depends on failing is 502 everywhere** — proposals and storage now follow
  the rule UC-3 set.
- **The root compose file runs the whole stack**: Resume Processing, and the AI Service on scripted
  answers, beside RabbitMQ, MinIO, MongoDB and PostgreSQL. MinIO no longer publishes free images,
  so the image is Chainguard's build of it.
- The architecture page still had the gateway asking the Identity Service to validate every
  request; it now verifies the token itself, as the runtime view and the SOC table already said.

Outstanding: ADR-010 and the documents that change with it — the SOC table and the runtime view
still describe the REST handover; per-criterion verdicts in `screening.completed`, for filtering
on nice-to-haves; UC-3's shortlisted precondition.

---

## 2026-10-17 15:40 — Token checks stay at the gateway; interview notes complete UC-3

- **The services behind the gateway will not verify tokens.** The gateway checks every request
  and the services trust that they are reachable only on the deployment's private network. This
  is a deliberate reversal of two accepted positions: ADR-009 asks every service acting on the
  role to verify what it receives, and ADR-001 says enforcement cannot live at the gateway
  alone. The argument for it is that one deployment serves one company (ADR-006) and nothing
  but the gateway is published; the cost is that anything that reaches a service directly is
  already past every access check, and that the duplicated-enforcement risk ADR-005 names is
  traded for a network assumption. **It needs an ADR of its own** — the stack is append-only,
  so that record would state what it overrides. Until it exists, the gateway's README, the
  Identity Service's README, the runtime view, the SOC table and the ADR index all say the
  decision was taken and is unrecorded.
- ADR-009 therefore stays *Proposed* on a narrower question: everything but the services' half
  is implemented, and that half is now withdrawn rather than outstanding.
- **FR-3.5 is built, so UC-3 is complete.** A note is recorded against the question it answers,
  which is what keeps the evidence attached to the criterion that question was meant to test.
  Three rules worth stating: a note does not move the guide's revision, because the questions
  have not changed; it survives an edit that leaves its question as it was; and regenerating
  drops the notes, because the new questions are not the ones that were asked.
- Outstanding: an ADR for gateway-only verification; an ADR for Hiring's document store; an
  owner for the gateway (ADR-005 rule 5); UC-3's precondition, since shortlisting is not built.

---

## 2026-10-17 13:17 — Signing in, and interview guides in their own document store

- **UC-0 works end to end**: the Identity Service issues the tokens ADR-009 describes and the
  gateway verifies them. Two decisions the record left to implementation: the role travels in
  the token as **Admin** and **Recruiter**, as the requirements name them, while the database
  keeps its lower-case spelling — one mapping, in one function; and **changing a password is a
  public route**, because FR-0.5 says a member must replace a temporary password before signing
  in, so they cannot hold a token when they do it. The Identity Service asks for the current one.
- **Removal and a password change now end that member's sessions**, which is what bounds a
  removed member's access to the lifetime of the token they already hold (ADR-009).
- **ADR-009's second half is still open.** The Identity Service's own `/members` endpoints do
  not read the token yet, so role enforcement still lives only at the gateway. The record stays
  *Proposed* until the services behind it verify what they are sent.
- **UC-3 is built, and interview guides live in a MongoDB database of Hiring's own.** That was
  an open decision in the Service–Operations–Collaborators table — ADR-003 puts AI-derived
  documents in MongoDB, which was otherwise Resume Processing's store — and implementation has
  now answered it. **It wants an ADR.** Both documents say so rather than reading as settled.
- **A guide a Recruiter has edited carries no model attribution**, and a candidate has one guide
  per opening: regenerating revises it, and a second generation is refused rather than quietly
  replacing edits. The guide endpoints sit under `/job-openings/{id}/candidates/{candidateId}/`,
  which keeps every hiring path behind the gateway's single prefix.
- **Hiring reads a candidate's profile without their name or email address.** The Resume
  Processing Service returns both; UC-3 takes the skills and the resume text only. Fewer copies
  of a person's contact details, fewer places to erase them from.
- Outstanding: an ADR for Hiring's document store; the services' half of ADR-009; an owner for
  the gateway (ADR-005 rule 5); `recordInterviewNote()` (FR-3.5); UC-3's precondition — the use
  case says *shortlisted*, and shortlisting is not built, so nothing checks it.

---

## 2026-10-17 12:28 — The gateway verifies tokens itself; ADR-009 is half agreed

- **The API Gateway is written, in Go.** ADR-005 leaves the language to the service's owner and
  rule 6 wants the reason recorded: it is written rather than configured from an off-the-shelf
  gateway because ADR-009 asks it to verify tokens itself and FR-0.4 asks it to record a rejected
  request, which is code either way; Go for consistency with the two services it fronts.
- **ADR-009 is implemented at the gateway, and stays *Proposed*.** It becomes Accepted when the
  gateway *and* the services behind it verify tokens themselves — one half now does, the other
  has not started. A row in the INDEX living view says so. No ADR was edited.
- **The runtime view and the SOC table no longer have the gateway calling
  `Identity.validateSession()` on every request** — ADR-009 named both as artifacts it contradicts,
  and the code now settles it. The operation itself is left in Identity's list until the ADR is
  accepted.
- The root README lists the gateway, and the retired `HireAssist-SOC.docx` row is replaced by the
  two builders.
- Outstanding: an owner for the gateway (ADR-005 rule 5); the services' half of ADR-009; sign-in
  in Identity, without which `/auth/sign-in` has nothing behind it; `recordAuthorisationRejected()`
  is a log line until Compliance & Insights exists.

---

## 2026-10-16 20:39 — The progress demo's scope recorded; the proposal's ADR index completed

- **The progress brief is recorded in `course/ASSIGNMENT.md`.** The clip shows UC-1 to UC-3 plus
  signing in: the three use cases the AI Service serves, which chain into one flow and between
  them use both PostgreSQL and MongoDB.
- **The broker sits between Hiring and Resume Processing**, carrying the screening handover — the
  team's division of work, rather than the pipeline events to Compliance & Insights the documents
  had named as its likeliest place. ADR-010 is to record it.
- **The proposal's ADR index lists ADR-007 to ADR-009**, which it had been missing. CONTEXT and
  the assignment status table now count 50 functional requirements, not 44; the status table
  also stopped calling the use case diagram an SVG and counting six ADRs.
- **The Identity Service owns members, not accounts and memberships** — the synonyms the glossary
  rules out — in the runtime view and on the architecture page. The runtime view's UC-6 flow also
  carried an old title and three of its six operations.

Outstanding: ADR-010, and the documents that change with it.

---

## 2026-10-15 19:52 — Hiring Service stores job openings in PostgreSQL

- Job openings and their criteria now live in PostgreSQL, implementing ADR-003. Criteria are rows in their own table rather than a `jsonb` column, because ADR-003 puts weighted criteria in the system of record, where constraints enforce them. No new ADR: the store was already decided.
- A root `docker-compose.yml` runs the Hiring Service with its database for local development. It is local tooling, not a deployment decision; ADR-005 rule 4's Kubernetes manifests are still to come. The Identity Service keeps its own compose file for now.
- The schema is applied idempotently at startup, with no migration tool yet. That is enough until the first incompatible change.
- Outstanding: whether the root compose file absorbs the other services; a readiness endpoint (ADR-005 rule 4); a migration tool.

---

## 2026-10-03 — Two deliverables: the proposal document and the ADR document

- **The submission is now two Word documents and nothing else** — `HireAssist-Proposal.docx` and
  `HireAssist-ADRs.docx`, each sent on its own. Both are built to stand alone: neither names a
  file, a diagram source or a repository path the reader was not given.
- **The architecture diagram and the Service–Operations–Collaborators table moved into the
  proposal**, as a closing landscape section — the collaborators column does not fit in portrait.
  The separate SOC document and its builder are deleted; one deliverable cannot be two files, and
  a builder that still produces the retired one is a second thing to mistake for current.
- **The ADR document gained the stack's superseded view.** Because an earlier record is never
  edited, ADR-001 still reads *"every boundary is REST, no exceptions"*; without the *What later
  records have changed* table a reader takes that as current. It is lifted from `adr/INDEX.md`
  at build time rather than retyped. Its title-page date was hardcoded to the first session and
  now follows the newest record.
- **The nickname column is gone from Group Members** — it identifies nobody to a marker.
- *Scheduler* became *System Scheduler* in the proposal's use case table, matching every other
  place the actor is named. Outstanding: the architecture drawing still says *Scheduler*.

---

## 2026-10-03 — New architecture diagram; references repointed from SVG to PNG

- **The architecture diagram is back in the repo** as `diagrams/sw-arch.drawio.png`, drawn in
  draw.io with the source kept outside it. Checked edge by edge against the
  Service–Operations–Collaborators table: all seventeen collaborations present, correctly
  directed, no extras. It gained the Scheduler, Hiring's email adapter and the
  `reportScreeningResult()` callback after a first pass found those three missing.
- `ARCHITECTURE.md` no longer describes a dashed external-systems box the new drawing does not
  have. An adapter box now stands for the external system behind it, and the Scheduler is
  explained — it is drawn because it is the only caller that bypasses the gateway, and without
  it the erasure arrows out of Compliance appear to fire from nowhere.
- **The use case diagram moved from SVG to PNG and four references were left pointing at the
  deleted file** — `PROPOSAL.md`, the `.puml` header, and two places in `CLAUDE.md`. The Word
  export was the costly one: it rendered the proposal from the SVG, so it silently shipped with
  no use case diagram and only a warning that scrolled past. It now embeds the PNG directly and
  **raises** when the image is missing, because a build that quietly drops a figure is worse
  than one that stops.

- **The superseded `diagrams/architecture-diagram.svg` is deleted.** Nothing referenced it once
  the new drawing landed, and a second architecture diagram in the repo is a second thing that
  can be mistaken for current. It remains in Git history.

Outstanding: the diagram labels the trigger *Scheduler* while every document calls it the
*System Scheduler*.

---

## 2026-10-02 20:23 — README lists the Identity Service

- `services/identity/` added to the README's documentation table beside the AI Service, the
  pattern that service set, so a reader finds every implemented service in one place.

---

## 2026-10-02 20:20 — OpenAPI contract for the Hiring Service

- `services/hiring/api/openapi.yaml` written and enforced by a contract test that validates every request and response against it — ADR-005 rule 1 makes the spec authoritative; it came after the endpoints this time, so from here the spec changes first.
- Swagger UI served at `/docs` as a developer convenience; whether the Gateway exposes it is undecided.
- Added `listJobOpenings()` to the SOC table — the code serves a list and the table had no operation for it.
- Pause, resume and close stay as POST actions, after comparing PUT and PATCH on a status field: they are rule-bound, non-idempotent business events (a repeat is 409, FR-1.9) that map one-to-one onto SOC operations; PUT would force a repeated pause to succeed silently. PUT stays for whole replacements (criteria).
- Outstanding: the spec's location is provisional until the repository-structure ADR decides where shared OpenAPI definitions live.

---

## 2026-10-01 — ADR-008: turning a resume into a profile is a model call

- **ADR-008 recorded.** Rule-based structuring of a resume was rejected because the layout is
  chosen by the candidate and cannot be specified in advance — and because it fails *silently*: a
  mis-associated job produces a well-formed profile, scored with confidence, which is the
  product's central failure reintroduced one layer down. The AI Service gains a fourth operation,
  `extractProfileFromText()`; PDF-to-text stays deterministic and in Resume Processing, so no
  model receives a file. The profile is extracted once per resume and cached; scoring stays once
  per resume per opening.
- **NFR-01 raised from 5 to 20 seconds at p95, abandoning at 60** — the same bounds as scoring,
  because it is now the same kind of work. The 5-second figure assumed text processing.
- **NFR-14's protection weakens from structural to instructed**, accepted deliberately: excluded
  attributes could not previously reach a prompt because the profile has no field for them;
  extraction now sees the whole resume and is instructed to drop them. Recorded in ADR-008 rather
  than glossed.
- Per the append-only rule, ADR-001, ADR-004 and ADR-005 are untouched; what each no longer holds
  is in the index. ADR-001's "opposite resource profiles" argument for the service split is the
  notable casualty — both sides now wait on a model call.
- Corrected in `OVERVIEW.md`, the SOC table and `ARCHITECTURE.md`, each of which asserted that
  profile extraction is not a model call.

- **`ExtractProfileFromText` implemented**, so the contract no longer contradicts the record.
  The `.proto` comment that called parsing deterministic is corrected; the SOC table and the
  runtime view carry the fourth operation.

Outstanding: extraction quality is unmeasured — no resume has been through the prompt, and the
protected-attribute instruction is asserted in a test against the prompt text, not against a
model's behaviour.

---

## 2026-09-28 13:25 — Word proposal extended to use cases and requirements

- Added *Scenario (use-case & description)*, *Functional Requirements* and *Non-functional
  Requirements* to `HireAssist-Proposal.docx`, in the brief's order. The use case diagram is
  rendered from the SVG at build time rather than kept as a committed PNG — the PlantUML source
  stays the only thing maintained.
- **The 44 FRs and 17 NFRs are parsed from their own documents, not retyped**, so the export
  cannot drift from what owns them. Editing a requirement in Word would be editing it in the
  wrong place. The prose sections remain condensations of `PROPOSAL.md`.
- The NFR file's *architecturally significant* table is excluded: its rows list several ids at
  once and are questions for ADRs, not requirements.
- *Group Members* added under *Project Name* rather than as its own heading, and likewise read
  from `PROPOSAL.md` — a student id mistyped into a build script is a mistake nobody proofreads.

Outstanding: the export has no *ADRs* section, so it is not yet the whole brief; the changelog's
six empty `---` separators and out-of-order entries are still there.

---

## 2026-09-27 03:32 — Short proposal exported to Word

- `docs/HireAssist-Proposal.docx` — Project Name, Problem Description and Target Customers only,
  condensed to about two pages because the full section runs long for a reader meeting the project
  for the first time. Built by `docs/build-proposal-docx.py`.
- It is a **condensation, not a conversion**: `PROPOSAL.md` stays the source of truth and the two
  can drift. Regenerate the .docx rather than editing it in Word — the same rule as the SOC export.

Outstanding: the proposal's remaining sections are not in the export; the changelog has six empty
`---` separators around line 96 and its newest entries are not all at the top.

---

## 2026-09-26 15:51 — Closed openings are final; first Hiring Service code

- New FR-1.9: a closed job opening cannot be paused, resumed, closed again or have its criteria revised — its outcome is what UC-4's metrics count; UC-1's prose in PROPOSAL says the same, and the FR counts are now 45.
- Criterion field named *classification*, not *priority*, following FR-1.2/FR-1.4 and the glossary — a must-have is a gate, not a ranking.
- First code: the Hiring Service's job opening operations in Go under `services/hiring/`, built test-first, clean architecture with dependencies injected at one composition root; in memory only. Go is the owner's choice under ADR-005, which stays *Proposed*.
- Deliberately not built: `proposeJobOpening()`, `recordPipelineEvent()` (OVERVIEW says it needs an ADR first), role enforcement, PostgreSQL.
- Outstanding: the repository-structure ADR (README promised it before code landed; `services/<name>/` is a working layout until then); an OpenAPI contract for the endpoints (ADR-005 rule 1).

---

## 2026-09-24 22:36 — ADR-009 proposed; *Member* confirmed as the Identity term

- **ADR-009**, *Proposed*, answers the open candidate "session and token mechanism for UC-0": a
  15-minute signed access token verified by the gateway and by any service acting on the role, and
  an 8-hour session held as single-use refresh tokens in Identity. Chosen by the Identity Service
  owner to take Identity off the path of every request and to give the role a verifiable way to
  reach the services; the cost, a removed member keeping access for up to 15 minutes, is argued in
  the ADR.
- The public key reaches verifiers as configuration rather than from a key endpoint, so no
  service calls Identity to check a token and the gateway stays its only caller; rotating the key
  becomes a configuration change, accepted because it is rare.
- Proposed rather than accepted because it asks the gateway and the other services to verify
  tokens themselves, and their owners have not agreed yet. INDEX row added; no earlier ADR edited.
  Drafted as ADR-008 and renumbered before it was committed, because the profile-extraction record
  took that number first on main.
- **Deliberately not updated until the team agrees:** OVERVIEW, the SOC table and ARCHITECTURE.md,
  which show the gateway calling `validateSession()` on every request; UC-0 alternate flow 4a,
  since an expired access token no longer ends the session; the INDEX candidate list and its
  ADR-005 row; and the open session decision in CONTEXT.md.
- **Member** added to the glossary as the term for a person with credentials and one role;
  *account* and *membership*, which named the same thing, are dropped from UC-6, FR-6.1 and the SOC
  Identity row. *User* was tried first and dropped: it meant renaming UC-6 and the Identity
  operations across documents the team shares, and the documents already use *user* in its
  everyday sense. A blank line that split the glossary table was also removed.
- **Removing a member deactivates it rather than deleting it** (new FR-6.2, a sentence in UC-6,
  the glossary). The access audit log must name whoever reached candidate data for a year (NFR-12)
  and every decision must trace to an identified person; a deleted row would leave both pointing
  at nobody.
- **The last active Admin cannot be removed or demoted** (new FR-6.3, UC-6 alternate flow 3a),
  because nothing inside the product can create an Admin again. Answers the open question in
  CONTEXT.md, which is removed along with the one on how a new member gets a password — FR-6.1
  already answers it.
- **An Admin can set a new password for a member** (new FR-6.4, UC-6, `setMemberPassword()` in
  the SOC Identity row), ending that member's sessions. Without it a forgotten password meant
  removing and re-adding the member, splitting their history across two identities; it is not a
  self-service reset, so UC-0's "no password-reset flow" still holds. Email stays unchangeable for
  now.
- **A password an Admin sets is a Temporary Password** that cannot start a session until the
  member replaces it (new FR-0.5, UC-0 alternate flow 1b, `changePassword()` in the SOC Identity
  row, glossary term; *initial password* replaced by *temporary password* in UC-6, FR-6.1 and
  FR-6.4). Otherwise the Admin, and anyone who read the message carrying it, could act as that
  member and the audit log would name the wrong person. FR-0.5 is free: the earlier one was
  withdrawn before the 2026-09-12 renumbering and nothing references it.
- **Reading members** (new FR-6.5, UC-6 step 1): `listMembers()` shows an Admin the active members;
  `getMember()` replaces `getMemberRole()`, which nobody needs once the role travels in the token,
  and still returns a removed member so history can name them (FR-6.2). Both are Admin-only until a
  screen needs to show Recruiters other members. Proposal total now 49 requirements.

Outstanding: team agreement on ADR-009, then the documents above; OVERVIEW and ARCHITECTURE still
call Identity's store accounts and memberships; README, CONTEXT's Requirements section and
ASSIGNMENT.md still count 44 requirements, not 49; OVERVIEW's UC-0 and UC-6 flows do not list
`changePassword()` or `setMemberPassword()`; the web application needs a screen for replacing a
temporary password; how long a removed member's email is kept is open. Parked for a team
discussion: how calls made without a user — the scheduler, erasure, the screening callback — prove
they come from our own services, which ADR-009 does not cover.

---

## 2026-09-24 18:09 — UC-1 split into propose and create

- Added `Hiring.proposeJobOpening()` to the SOC table; OVERVIEW's UC-1 sequence now shows two operations instead of the AI call and the Recruiter's review inside `createJobOpening()` — one request cannot stay open while a person edits, and FR-1.3 needs the proposal to exist before the opening does.
- `createJobOpening()` makes no AI call, so manual entry (FR-1.5, flow 4a) works when the AI Service is down.
- Considered and rejected: the Gateway calling the AI Service directly — ADR-001, ADR-004 and ADR-007 keep it internal, and ADR-007's case for gRPC rests on no browser reaching it.
- Outstanding: the Hiring Service Go code in `services/hiring/` is untracked in the docs until its first iteration lands; the repository-structure ADR and ADR-005's language choice are still open.

---

## 2026-09-12 12:59 — Diagram: no arrow crosses a box

- Hiring's two adapters stacked on its right edge (they sat side by side, and one adapter's arrow ran through the other); the Hiring → AI Service arrow now leaves from the top corner. Cosmetic.

---

## 2026-09-12 12:57 — Diagram: System Scheduler aligned with Compliance & Insights

- Actor moved level with the service it calls so the arrow is horizontal. Cosmetic.

---

## 2026-09-12 12:52 — Architecture diagram laid out left to right

- AI Service moved out of the service column into its own column between the domain services and
  the external systems, and Identity placed under the gateway (the only thing that calls it). The
  picture now reads left to right — actors → UI and gateway → domain services → AI Service →
  external systems — and is a third shorter. Content unchanged.

---

## 2026-09-12 12:36 — Web UI renamed

- *Recruiter Web UI* → **HireAssist Web UI** on the architecture diagram and page: it is the one
  web application for every signed-in role, not a Recruiter-only screen.

---

## 2026-09-12 12:25 — Version 1 diagram shows REST for every call

- The course's guidance for the first version is REST throughout and no message broker yet, so
  the diagram now draws every call as REST: the per-resume hand-off is `screenResume()` /
  `recordScreeningResult()`, Compliance reads pipeline events and rejected sign-ins through
  `getPipelineEvents()` and `listAuthorisationRejections()`, and the AI Service exposes a REST API.
  ADR-001 and ADR-002 stand — gRPC and RabbitMQ are drawn in a later version; `ARCHITECTURE.md`
  says so.
- The table in PR #4 marks these collaborations (gRPC) and (MQ); it should say (REST) and name the
  same operations.
- Detail cut to the FTGO example's level: service names, API tabs, stores, adapters, actors and
  arrows — no operation labels, no legend. The operations live in the table and in the use case
  traces on `ARCHITECTURE.md`.

---

## 2026-09-12 11:55 — Architecture diagram redrawn by hand in the FTGO style

- `docs/diagrams/architecture-diagram.svg` is now a hand-laid SVG shaped like the course's FTGO
  example — hexagon services with API tabs, private stores inside them, adapters on the edge,
  external systems in a dashed box, actors on the left. Graphviz could not produce that shape, so
  the PlantUML source was dropped; the SVG is the source and is edited directly.
- Content unchanged from 11:40: same services, stores, calls and event names.

---

## 2026-09-12 11:40 — Architecture diagram, version 1

- **`docs/ARCHITECTURE.md` and `docs/diagrams/architecture-diagram.{puml,svg}`** — the diagram half of
  the *Microservice Design with Collaborations* deliverable, drawn to ADR-001…006 and to the
  operations in the Service–Operations–Collaborators table (PR #4). Arrows follow the course
  convention (A → B means A calls B; no response arrows); the page traces UC-1, UC-2 and UC-3
  actor → operation → service → collaborator → store.
- Drawn from the accepted decisions where the table still lags them: *Identity Service* not
  *Identity & Workspace*, no workspace at the gateway (ADR-006), and no consent operations — consent
  is not stored (23:34 entry). The table needs the same fixes before submission.
- **No external OCR provider drawn.** The table lists a *Document Parsing / OCR Provider*; ADR-002
  sends a scanned image to manual review as a permanent failure, and a second processor of resume
  data would need its own decision. Parked in `CONTEXT.md`.
- RabbitMQ stays in version 1 although the course allows REST-only: it is the decided UC-2 design
  (ADR-002), not decoration — the page says why.
- `README.md` links the new page.

**Outstanding:** bring the table in line with ADR-006 and the consent change; decide the OCR
question; service ownership and the erasure verification pass are still open.
---

---

---

---

## 2026-09-24 14:10 — Two open decisions settled by starting implementation

Work on the first service answered two questions the documents had left open. The code itself is in
the commit; what belongs here is what it decided.

- **Repository layout.** Services live at `services/<name>/`, shared gRPC contracts at `proto/` in
  the repository root — callers generate clients from the same file the owning service serves, so
  the contract cannot sit inside one service's directory. This was an open question in CONTEXT.md
  and adr/INDEX.md, and ADR-007 made it urgent by needing a home for the `.proto`. Settled by fact
  rather than by decision, so it still needs a record.
- **Python for the AI Service.** ADR-005 leaves the language to each service's owner and is still
  *Proposed* pending ownership. Its preference table already called this one "strong — toward
  Python", so nothing contradicts — but one service choosing does not let ADR-005 move to Accepted.

Also: README gains rows for `services/ai-service/` and `proto/`, and CLAUDE.md no longer describes
the repo as documentation only.

**Changelog scope narrowed.** This log covers the documentation; Git history covers the code.
Scaffolding a service is a commit message, not an entry here — but a decision the code settles,
like the two above, is documentation and still belongs. Recorded in CLAUDE.md.

---

## 2026-09-20 11:20 — The committed architecture diagram is deprecated

The diagram is being redrawn outside the repo, so `docs/diagrams/architecture-diagram.svg` is now
the old version. Recorded in CLAUDE.md's *Context outside this repository* table: the known defects
in it — the Compliance to Identity arrow, the missing Hiring to Compliance arrow, the stale header
comment, and the two AI Service arrows that ADR-007 makes gRPC — stay recorded but are not being
fixed here. `docs/ARCHITECTURE.md` still embeds it and gets revisited when the replacement lands.

---

## 2026-09-20 11:05 — ADR-007: gRPC on the AI Service boundary

A new record rather than an edit to ADR-001 — the stack stays append-only, and ADR-001 keeps saying
what it said on the day.

**The decision.** The AI Service speaks gRPC; every other boundary stays REST. Three RPCs in a
`.proto`, generated clients on both sides, field numbers never reused, still unreachable from the
gateway.

**Why this boundary.** Highest call volume in the system (once per resume, not once per batch); the
narrowest and least forgiving contract, where a renamed field silently costs a justification rather
than raising an error; and the boundary most likely to be crossed by two different languages under
per-service language choice. ADR-001 had already named it as the first to revisit.

**Recorded honestly in the ADR:** "easier to consume" is normally the argument *against* gRPC — it
is worse for a browser, an outside integrator or anyone with `curl`. It works here only because the
boundary has none of those consumers. Inside the system, easy to consume means a typed client the
caller did not write. Also recorded: a gRPC service is required of us and none existed, so the
record is convenient as well as correct; what makes it defensible is that the boundary was chosen
on properties that would hold if the requirement did not exist.

**Cascade into the living documents** — no ADR was edited:
- **INDEX.md** — new row, and "What later records have changed" updated for ADR-001 (the REST rule
  now has one exception), ADR-004 (same operations, different transport) and ADR-005 (its contract
  guardrail now covers OpenAPI *and* a `.proto`).
- **SOC table** — the two AI Service collaborations marked *(gRPC)*; the header no longer claims a
  single protocol.
- **ARCHITECTURE.md** — reading key and the protocol section rewritten; the section is now "Why
  REST everywhere except the AI Service".
- **OVERVIEW.md** and **CLAUDE.md** — the blanket REST statement narrowed; the outstanding gap is
  now the message broker alone.

Outstanding: the `.proto` has no home yet (the repository-structure question is now urgent), and
the diagram's two AI Service arrows need a gRPC label — on top of the four SVG edits already open.

---

## 2026-09-20 10:30 — ARCHITECTURE.md trimmed to the diagram; traces live in OVERVIEW.md

Two prose descriptions of the same runtime behaviour is the drift surface that caused the PR #5
mess in the first place, and adding OVERVIEW.md on 12 Sep created a second one.

- **ARCHITECTURE.md** loses its three use-case traces (46 lines) and points at OVERVIEW.md, which
  covers all seven use cases rather than three. It keeps what is tied to the picture: the diagram,
  the reading key, the actors table, the services-and-stores table, and why every boundary is REST.
  93 lines, down from 129.
- Considered deleting the file outright — rejected. It is the only thing that renders the SVG, and
  an architecture diagram is a required deliverable; deleting it would orphan the image.
- README's one-line description updated to match.

Outstanding: the four SVG edits — the Compliance→Identity arrow, the missing Hiring→Compliance
arrow, the stale header comment and footer, and the gateway→Resume Processing arrow (which should
wait until the routing question is settled rather than deciding it by accident).

---

## 2026-09-12 06:48 — ARCHITECTURE.md brought in line with the rest

PR #5 was branched before the REST-only change and merged rather than rebased, so the page still
described the pre-REST design. Eighteen corrections; the SVG is deliberately left alone for now.

- **The protocol story.** "Why version 1 is REST-only" claimed the course asks for REST first and
  brokers later — it does not; it asks for a spread of styles, and ADR-001 records our failure to
  meet that as deliberate. Replaced with "Why every boundary is REST": the decision, the reasoning
  (boundaries are expensive to move, protocols are not), and the gap stated plainly as a gap. The
  reading key no longer promises gRPC and RabbitMQ in a later version.
- **Six operation names** corrected: `screenResume`→`submitResumeForScreening`,
  `recordScreeningResult`→`reportScreeningResult`, `recordDecision`→`shortlistCandidate`,
  `extractProfileFields` removed (extraction is deterministic and in-process), and
  `getPipelineEvents`/`listAuthorisationRejections` replaced by the pushes that really happen.
- **Design errors.** Criteria travel in the hand-off body, not fetched per resume; the
  justification stays in Resume Processing; the UC-2 trace now describes the work table, the
  criteria snapshot and the *needs manual review* terminal state.
- **Three stores were missing** from the service table: Resume Processing's PostgreSQL, Identity's
  memberships, Compliance's access audit log.
- **Two notations the key described but the diagram never draws** — ownership lines and a dashed
  deployment boundary — reworded to describe what is actually drawn, since the SVG is untouched.
  The gateway is now "single entry point for user traffic", which the System Scheduler no longer
  contradicts.

Outstanding: six SVG findings — the Compliance→Identity arrow, the missing Hiring→Compliance
arrow, the stale header comment and footer, and the two absent notations.

---

## 2026-09-12 06:22 — SOC table exported to a Word document

- **docs/HireAssist-SOC.docx** — the Service–Operations–Collaborators table alone, landscape, with
  the header row repeating across pages. The notes, the gateway paragraph and the open-decisions
  list are deliberately left out; the markdown keeps those.
- Generated by `docs/build-soc-docx.py` from the markdown, which stays the source. Regenerate
  after any change rather than editing the .docx.

---

## 2026-09-12 06:05 — ADRs exported to a Word document

- **docs/HireAssist-ADRs.docx** — all six records in one file: title page, a contents table with
  each record's status, then ADR-001 to ADR-006, one per page. Generated from the markdown by
  `python-docx` (no pandoc on this machine), so headings, tables, quotes and inline code are real
  Word styles rather than a conversion artifact.
- The markdown in `docs/adr/` stays the source. Regenerate the .docx after any ADR change rather
  than editing it — it carries no content of its own.

---

## 2026-09-12 05:38 — ADRs are append-only; amendment notes removed

An ADR is a record made on a date, and a later decision must not reach back into it. Four dated
*"Amended by ADR-NNN"* lines had been added to the Status of ADR-001 (x2), ADR-003 and ADR-005 —
each an older record edited to know about a newer one. All removed, with ADR-001's pointer to them.

- **TEMPLATE.md, CLAUDE.md** — the "Status is the exception" rule is replaced: nothing in an
  earlier ADR is edited because of a later one, Status included. To change a decision, push a new
  record naming the earlier one and stating what it overrides; that record is the only file
  written. **No *Superseded* status** — marking the old file would itself be reaching back into a
  finished record to point at the future. Revise an ADR only when explicitly asked.
- **INDEX.md** — new "What later records have changed" table, per record: what still holds and
  what a later ADR changed. This is where the amendment notes' information now lives. INDEX is a
  living document; the ADRs are not.
- ADR-006's "That record is amended accordingly" and ADR-005's "whose Assumptions this record
  amends" reworded — pointing backwards is fine, implying the earlier file gets edited is not.

---

## 2026-09-12 05:10 — Added docs/OVERVIEW.md, the runtime view

Nothing described how the system behaves call by call: the SOC table is static structure,
PROPOSAL.md is user-facing scenarios, the ADRs give reasons. OVERVIEW.md fills the gap — per
service what it owns / does / is called by / calls, then the request sequence behind UC-0 to UC-6.

Explicitly **not a source of truth**, and says so at the top: it defers to PROPOSAL for use cases,
the SOC table for operations, the ADRs for rationale. That precedence is stated in the document
and in CLAUDE.md, because a second description of the same system is the thing that drifts.

---

## 2026-09-12 04:26 — Chain audit: nine stale references corrected

Independent audit of PROPOSAL → use cases → FR → NFR → ADR → SOC. The renumbering held: all ~40
requirement citations were re-read sentence-against-requirement and none miscites. Everything found
was stale text, not design error:

- **CONTEXT.md** — the OCR open question described an external provider the SOC table no longer
  lists; the candidate-register question said the record is owned by no service when the SOC table
  assigns it to Resume Processing; the FR count said 50, not 44; the "decided on 2026-09-11" list
  omitted ADR-005 and ADR-006; one question was gated on the SOC table, which now exists.
- **PROPOSAL.md** — status line said four ADRs; there are six.
- **INDEX.md** — said eight candidate decisions where the table holds nine, and called them
  answered by the records, which by construction they are not.
- **ADR-006's title** was written three ways; all now use the record's own H1.
- **SOC table** — Resume Processing's ownership line now reads "per-criterion scoring output", so
  it no longer scans as owning the headline score Hiring holds.

---

## 2026-09-12 03:52 — All requirement blocks compacted

Extended the UC-5 renumbering to the rest; no block now has a gap.

`FR-0.4→0.3 · 0.6→0.4` · `FR-4.6→4.5 · 4.7→4.6`

UC-1, UC-2, UC-3 and UC-6 were already contiguous. References rewritten in PROPOSAL.md,
NON-FUNCTIONAL-REQUIREMENTS.md, adr/INDEX.md and ADR-001, ADR-003, ADR-005.

**The hazard this created.** FR-0.3 was withdrawn by ADR-006 and three ADRs still named it. After
compaction that id belongs to the two-roles requirement, so those references would have resolved
silently to the wrong requirement — worse than a dangling id, because no check catches it.
ADR-001, ADR-005 and ADR-006 now name it in words ("workspace scoping"). CLAUDE.md records the
rule: a withdrawn requirement is never referred to by id after a renumbering.

---

## 2026-09-12 03:34 — UC-5 requirement ids compacted

Three withdrawals had left the retention block reading 5.1, 5.2, 5.5-5.7, 5.9-5.11. Renumbered
contiguously by decision, while nothing has been submitted:

`FR-5.5→5.3 · 5.6→5.4 · 5.7→5.5 · 5.9→5.6 · 5.10→5.7 · 5.11→5.8`

This breaks the stable-id rule deliberately and edits requirement references inside ADRs, which
are point-in-time records. Defensible only because nothing is submitted yet.
FUNCTIONAL-REQUIREMENTS.md and CLAUDE.md record it as a one-off, not a precedent.

---

## 2026-09-12 03:20 — Stale requirement id in the NFR document

- **NON-FUNCTIONAL-REQUIREMENTS.md** — the PDPA note cited "FR-5.1 to FR-5.12"; the retention block
  ends at FR-5.11. Corrected. Pre-existing, left behind when FR-5.12 was withdrawn.

---

## 2026-09-12 02:48 — Hiring reads the candidate profile from Resume Processing

Walking UC-1 → UC-3 end to end exposed a missing edge: Hiring needs the candidate profile to
generate an interview guide (UC-3 main flow step 2) and the written justification when a Recruiter
opens a screened candidate, but both are Resume Processing's documents and the SOC table gave
Hiring no read path to them.

- **SOC table** — added `Hiring → ResumeProcessing.getCandidateProfile()`, with a note on why.
- Rejected the alternative of carrying the justification back in `reportScreeningResult()` and
  storing it in Hiring: that puts an AI-derived document in the relational system of record,
  against ADR-003's split, and creates a second copy to erase and to disagree with the first. The
  ranked list is unaffected either way — score and must-have check are Hiring's own columns.

## 2026-09-12 02:20 — REST on every boundary; the broker and gRPC are deferred

Nothing has been submitted yet, so the affected records were revised in place rather than
superseded.

- **ADR-001** — one protocol on every boundary, REST over HTTP/JSON, replacing REST + gRPC + MQ.
  Boundaries are the expensive thing to get wrong and a protocol is not, so we place the
  boundaries first and add protocol variety when a boundary demonstrates it needs it. New
  rejected position 7 (protocol per boundary) carries the counter-argument.
- **ADR-002** — rewritten. The queue moves inside Resume Processing as a claimable work table
  (`SELECT … FOR UPDATE SKIP LOCKED`); Hiring hands over one resume per acknowledged REST call and
  Resume Processing calls back with the result. What survives unchanged is the shape that was
  actually argued: one unit of work per resume, at-least-once, idempotent upsert, classified
  retries, a terminal state for work that will not succeed. Title and INDEX row updated.
- **ADR-004** — the AI Service's three operations become REST endpoints; unchanged in substance.
- **ADR-005** — guardrail 1 is now OpenAPI-only and is load-bearing rather than tidy: with no
  schema-carrying wire format, the document is the only thing preventing contract drift.
- **ADR-003, ADR-006** — incidental broker and gRPC mentions removed.
- **SOC table** — protocol markers dropped, `ResumeSubmitted`/`ResumeScored` replaced by
  `submitResumeForScreening()` and the `reportScreeningResult()` callback, domain events replaced
  by `recordPipelineEvent()`. New open decision recorded: pushing pipeline events over REST makes
  Compliance a synchronous dependency of Hiring's write path with no retry story.
- **Criteria ownership settled:** they stay with the Job Opening in Hiring. Resume Processing
  receives a snapshot in the handover body and holds it on the work row for the life of the entry
  — never fetching, never owning. The snapshot still pins one criteria version per resume, so a
  mid-batch edit cannot invalidate a justification.
- **CLAUDE.md** — records the divergence explicitly: the course's REST + gRPC + broker spread is a
  known open gap, not something to design around, and reinstating either protocol is an ADR.

Outstanding: service ownership still blocks ADR-005; the erasure cascade, pipeline-event delivery
and the interview-guide document store still need records; the independent audit of the
UC → FR/NFR → ADR → SOC chain has not been run.

## 2026-09-12 01:02 — Service–Operations–Collaborators table corrected

Six rows described a system we no longer have, because the table was written against earlier
versions of the documents:

- Identity **& Workspace** Service, and the gateway "resolving the workspace" — the workspace
  concept was removed by ADR-006.
- `inviteMember()` — there is no invitation flow; an Admin creates the account with an initial
  password.
- Consent records and `recordConsent()` / `renewConsent()` — consent is obtained before a resume
  arrives and is not stored, and retention extension was removed as not defensible.
- `recordDecision()` — shortlisting is the only recorded decision; rejection is derived.

Four collaborations conflicted with an ADR and were changed:

- **Criteria now travel inside `ResumeSubmitted`** instead of Resume Processing calling Hiring per
  resume. A synchronous call would reintroduce the mid-batch failure the queue removes, and
  criteria edited mid-batch would mean two resumes scored against different versions.
- **`extractProfileFields()` dropped from the AI Service** — parsing is deterministic and
  in-process; ADR-004 defines three model operations and field extraction is not one.
- **OCR adapter dropped** — a scanned image with no text layer is a permanent failure by design,
  so an OCR provider contradicts the requirement rather than serving it.
- **Compliance no longer calls `getJobOpening()`** — the dashboard is a projection built from
  events, per ADR-001.

Added: `listHeldExpiries()` / `resolveHeldExpiry()` for the held-expiry case, justifications and
the candidate register under Resume Processing's owned data, and the `AuthorisationRejected`
publisher moved from Identity to the Gateway, where the rejection happens.

Kept from the original: the Email Adapter, which closes a notification gap no ADR owned.

Three decisions the table was making silently are now listed at the bottom of it as needing
records — erasure by orchestration, the candidate register's home, and a document store for
interview guides. The first is also added to `CONTEXT.md`.

---

## 2026-09-12 00:41 — Candidate record ownership parked as an open question

- FR-2.11 adds candidates to a "talent pool" that no service owns, and the pool is a view rather
  than a collection now that re-matching is deferred. Recorded in `CONTEXT.md` rather than acted
  on — the requirement works as written; only the ownership is unstated.

---

## 2026-09-12 00:16 — Changelog hook rewritten in POSIX sh

- The hook was Python; `python3` is not guaranteed on a teammate's machine, a shell is. Rewritten
  in POSIX `sh` with no interpreter and no `jq` dependency.
- Detection changed from "modified in the last 20 seconds" to "modified since the last check",
  using `find -newer` against a marker file — POSIX, and it drops the arbitrary time window while
  keeping the same behaviour: fires on a real change, silent on a read, reports a change once,
  silent when CHANGELOG.md changed too.
- Tested: first run, no change, read-only, single change, repeat, docs-plus-changelog, truncation
  at eight files, JSON validity, and a run under `dash` to confirm no bashisms.

---

## 2026-09-12 00:04 — README stops listing individual ADRs

- The README carried a table of every ADR, which had already drifted once — six records, a
  description of four. A README that mirrors another index is a second copy that goes stale. It now
  just says where the records live.

---

## 2026-09-11 23:52 — ADRs made self-contained and shorter; rejection derived, not recorded

- **ADRs no longer reference any project document.** They are read on their own — in review, by a
  new joiner, lifted out of the repository — so a record that depends on the proposal or the
  requirements file to make sense is broken the moment it travels. Facts are now stated in place;
  requirement ids stay for traceability but say what they mean. Rule added to the template.
- **All six compacted**, roughly 15% shorter with no loss of meaning: restatement cut, sentences
  tightened, the "one to two pages" guidance made explicit in the template.
- **Rejection is derived, not recorded.** A candidate missing a must-have is marked not qualified
  by screening; everyone else simply goes un-shortlisted. The Recruiter shortlists and can override
  a bad mark, so the reason someone did not advance is the same recorded justification for
  everyone rather than a free-typed note that varies with the reviewer.

**Outstanding:** service ownership unassigned (blocks ADR-005); erasure cascade and deployment
pipeline unwritten.

---

## 2026-09-11 23:34 — Retention extension removed (FR-5.4)

- **A Recruiter can no longer extend a candidate's retention.** Under the PDPA data may be kept
  only as long as the purpose it was collected for requires; keeping someone for a *future*
  opportunity is a new purpose needing a new basis, so a unilateral extension is exactly what the
  rule exists to prevent. Data now ages out on schedule, full stop.
- The requirement was originally *renew consent*, which was lawful. It became *extend retention*
  when consent storage was dropped earlier today — that edit quietly turned a defensible act into
  an indefensible one.
- **FR-5.9 remains the only way expiry is deferred**, and it is justified: it holds expiry for a
  candidate still in an active hiring process, where the original purpose has not ended.
- UC-5's warning window now says what it is for — act on anyone still in play before the date
  passes. 44 requirements; UC-5 is down to 8.

**Outstanding:** whether rejection is recorded by the Recruiter or derived entirely from screening
(FR-2.9) is still undecided.

---

## 2026-09-11 23:15 — Tenancy removed, ADRs 005 and 006, requirements tightened

- **ADR-006: one deployment per customer.** The workspace concept was never decided — it arrived
  with the multi-tenant framing and spread unchallenged. Single-tenant means a cross-customer leak
  cannot happen, at the cost of operating N instances. FR-0.3 withdrawn; UC-0 is now *Sign in* and
  UC-6 *Manage members and roles*; diagram re-rendered from source.
- **ADR-005: language per service — Proposed, not Accepted.** ADR-001 had asserted Go in its
  Assumptions. That assumption is struck; the languages themselves wait on service ownership.
- **ADRs live only in `adr/`.** The proposal's 144-line summary section is now a linked index, so
  a decision is stated once and cannot drift.
- **ADR-001's argument no longer cites the course requirement.** It now rests on fault isolation
  of the model path and the opposite resource profiles of parsing and scoring.
- **Six actor gaps closed.** No self-service sign-up (accounts are provisioned); erasure requests
  arrive by email and an Admin executes them; consent is obtained before upload and no longer
  stored; the *interviewing* stage is removed; closing a job records *filled* or *cancelled*.
- **Requirements tightened to 45.** UC-5 lost three restatements and UC-4 one; withdrawn
  requirements are now deleted rather than struck through, leaving gaps in the ids.
- **ADRs made time-ordered.** An ADR may only cite one that existed when it was written; later
  changes go in its Status as a dated amendment. Eleven forward references removed.
- Integrity pass: all FR, NFR, UC and link references resolve; counts agree everywhere.

**Outstanding:** service ownership unassigned (blocks ADR-005); erasure cascade and deployment
pipeline unwritten.

---

## 2026-09-11 — ADRs reconciled with the UC-0 / UC-6 split

Two strands of work ran in parallel today and were merged: the four ADRs (_First four ADRs
recorded_) and the use case work (_Use case diagram redrawn in UML, as code_, _UC-0 split_, and
_Invitation-email «extend» removed_). Each was written without knowing the other's result, so
each left statements the other has since made untrue. This entry reconciles them. The earlier
entries stay as written.

**The ADRs now credit memberships and roles to UC-6, not UC-0.** They were written against the
old UC-0, which bundled signing in with managing who belongs to a workspace. After the split,
UC-0 covers only authentication. So ADR-001's Identity & Workspace Service now serves UC-0 _and_
UC-6. ADR-003 lists workspaces and users under UC-0, and memberships and roles under UC-6. The ADR
summary in `PROPOSAL.md` says the same. ADR-003's "nothing in UC-0 through UC-5 needs to search
resume text" now reads UC-6, since there is one more active use case.

None of this changes a decision. Both ADRs already put identity, workspaces, memberships and roles
in one service, stored in PostgreSQL. UC-6 changes only which use case that behaviour traces to.
So the ADRs were corrected where they stand rather than superseded. Superseding is for a decision
that changed, and these were fixes to their traceability.

**Superseded statements in the earlier entries.** Two statements in earlier entries are no longer
true, and this entry supersedes them:

- _Use case diagram redrawn in UML, as code_ and _Invitation-email «extend» removed_ both close
  with "No ADRs are written yet." Four are now written.
- _First four ADRs recorded_ lists "the use case diagram needs redrawing in proper UML" as
  outstanding. The diagram is now redrawn in PlantUML.

The status line in `PROPOSAL.md` had carried the second statement forward. It now lists the
diagram as complete.

**The merge itself.** Git flagged only `CHANGELOG.md`, where both strands had added entries at
the same place. Both sets were kept unchanged. The use case entries sit above the ADR entry
because they reached the shared `main` later. The ADR and proposal fixes above are the conflicts
Git could not see: text that merged cleanly but no longer agreed.

**Still outstanding.** The two UC-6 questions in `CONTEXT.md`, about how an invited person sets a
password and whether the last Admin can be removed. When they are settled, the open decision on
the UC-0 session and token mechanism in `adr/INDEX.md` may need to cover the invitation flow too.
Also still open: the FR/NFR lists to be inlined into `PROPOSAL.md` before submission, the
load-test plan, the risk matrix, and the seven candidate decisions in `adr/INDEX.md`.

---

## 2026-09-11 — Invitation-email «extend» removed from the use case diagram

**_Draft Interview Invitation Email_ is gone from the diagram,** along with the extension point
_candidate shortlisted_ on UC-2 and the condition note on the arrow. This closes the gap left open
by the _Use case diagram redrawn in UML, as code_ entry below. That entry offered two ways out:
write the behaviour into UC-2 and add a requirement for it, or take the «extend» off the diagram.
We took the second.

The reason is consistency. The assignment asks for a diagram that agrees with the use cases, and
this was the only element with nothing behind it. UC-2's flows never mentioned an invitation, and no
functional requirement backed one; it survived only from the original idea list. Keeping it would have shown graders a feature the proposal does not deliver.
Adding it properly would have grown UC-2's scope for behaviour that does nothing for the core
problem of screening capacity.

The diagram now carries no «include» or «extend» relationships at all. The rationale in
`PROPOSAL.md` says so and explains why, as `docs/course/ASSIGNMENT.md` asks: they are not
required, and should not be added only to make a diagram look more complex. The provenance table in `CONTEXT.md` records
that the original _first draft email for interview meeting_ idea was dropped.

Removing the extension also cleaned up the rendered layout. The System Scheduler and Candidate
associations no longer cross the boundary at odd angles.

**Still outstanding.** The two UC-6 questions from the entry below, about how an invited person
sets their password and whether the last Admin can be removed. No ADRs are written yet.

---

## 2026-09-11 — UC-0 split: Guest signs in, Admin manages access (UC-6)

**UC-0 is now _Authenticate into a workspace_, and its actor is a new Guest.** The old UC-0 bundled
two things with different actors and different goals: every user signs in every session, while
only an Admin, occasionally, decides who belongs to the workspace. Keeping them together hid the
Admin's distinct responsibility inside a use case everyone takes part in, and made Recruiter and
Admin actors of their own sign-in, which is circular.

**The Guest establishes a convention the documents now rely on:** _Recruiter_ and _Admin_ always
mean a signed-in user. That is what lets UC-1 to UC-6 name those actors without repeating the
sign-in, and it is why UC-0 is drawn once, against the Guest, rather than as «include» arrows from
every other use case. No generalisation is drawn between Guest and Recruiter or Admin, since
signing in changes the role a person plays and a Recruiter is not a kind of Guest. The term is in
the glossary. UC-1 and UC-2 still list authentication as a precondition. Under the convention
that is redundant but not wrong, so it was left alone, and UC-0 says the other use cases _need not_
repeat the sign-in rather than that they never do.

**Managing access became UC-6, _Manage workspace access_, with Admin as its actor.** It took the
next free number rather than slotting in as UC-1. Inserting it would have shifted UC-1 to UC-5
and, through the `FR-<use case>.<n>` scheme, renumbered 44 requirements whose IDs are meant to be
stable, as well as every cross-reference to them. `CLAUDE.md` now states the rule so the next new
use case follows it. The cost is that the ID order no longer follows the order a user meets the
features.

**Requirements.** FR-0.5 (invite, remove, change role) was struck and moved unchanged to FR-6.1,
following the rule that a withdrawn ID is struck, never reused. FR-0.1 now authenticates a Guest
instead of "an Admin or Recruiter". FR-0.2, FR-0.3, FR-0.4 and FR-0.6 stay with UC-0: they govern
the session token and what every request under it may do, which is not specific to managing
members. The total is still 50.

**Diagram.** Guest was added with its association to UC-0. Recruiter no longer associates with UC-0.
Admin gained an association with UC-6. Admin still specialises Recruiter, now inheriting UC-1 to
UC-4. Guest and UC-0 sit at the bottom of the rendered image, because they are not connected to
the rest of the graph and Graphviz lays them out last. Several hidden-link layouts were tried to
move them to the top, and each one scrambled the use case order or crossed lines, so they were
dropped. Position carries no meaning in a UML use case diagram.

Nothing else in the use cases changed, as the feedback asked.

**Still outstanding.** Two gaps that splitting UC-6 out made visible, both now open questions in
`CONTEXT.md`. First, nothing covers how an invited person sets the password UC-0 checks. Second,
nothing stops the last Admin being removed or demoted, which would leave a workspace that no one
can administer. UC-6 has a single requirement, so these may justify more. The invitation-email
«extend» gap from the entry below is still open.

---

## 2026-09-11 — Use case diagram redrawn in UML, as code

**The use case diagram now lives in its own file as PlantUML**, at
`docs/diagrams/use-case-diagram.puml`, with an SVG rendered from it. `PROPOSAL.md` embeds the SVG
and links to the source. The inline Mermaid block was removed rather than kept beside it: two
copies of one diagram would drift, which is the same failure the PROPOSAL/CONTEXT split exists
to prevent. The relationships rationale and the notes stay in `PROPOSAL.md`, because they are
settled content a grader reads next to the picture.

This closes the item both earlier entries left outstanding — redraw in proper UML for submission.

**Why PlantUML and not Mermaid.** Mermaid has no use case diagram type; the draft was a
flowchart with rounded boxes standing in for use cases. It could not draw stick-figure actors,
actor generalisation, or an extension point, so it approximated UML rather than using it.
PlantUML has native use case syntax and draws all of those. It is still diagram-as-code, so the
diagram is diffable in Git and reviewed like any other document change.

The cost is rendering. GitHub renders Mermaid inline but not PlantUML, so the SVG must be
committed alongside the source and re-rendered after every edit — a stale image is a stale
diagram, and nothing enforces it. Step 4 of the use-case checklist in `CLAUDE.md` now says to
re-render and commit both files together. Anyone editing the diagram needs PlantUML locally
(`brew install plantuml`, or the jar with Java, or an editor extension).

**What the UML version shows that the draft could not:**

- **Admin is drawn as a specialisation of Recruiter** (actor generalisation), so it inherits the
  Recruiter associations UC-0 to UC-4, and only its own association to UC-5 is drawn. The draft's
  note described this relationship but the picture never showed it, and the note called Admin a
  _generalisation_ of Recruiter, which is the wrong way round — corrected to _specialises_.
- **UC-2 declares the extension point** _candidate shortlisted_, and the «extend» arrow from
  _Draft Interview Invitation Email_ carries its condition — the Recruiter chooses to invite a
  shortlisted candidate. The draft had the arrow but neither the point nor the condition.

No use case or actor was added or removed.

**Still outstanding.** _Draft Interview Invitation Email_ appears only in the diagram and its
rationale — UC-2's flows never mention it, and no functional requirement backs it. Either UC-2
gains an alternate flow and FR-2.x a requirement, or the «extend» comes off the diagram. No ADRs
are written yet.

---

## 2026-09-11 — First four ADRs recorded

*(Later the same day, on top of the* Requirements *entry below.)*

The architecture decisions that had been listed as *candidates* since the repository was created
are now decided and written up. Four records, deliberately read as a sequence: **ADR-001** draws
the service boundaries, **ADR-002** fills in the busiest one, **ADR-003** says what each side
stores, and **ADR-004** fills in the external dependency the other three are built to survive.
Every record maps its decision to the `FR-` and `NFR-` identifiers the *Requirements* entry
introduced, so traceability runs requirement → decision rather than decision → vague intent.

**Why four and not three.** The course minimum is three. A fourth was written because the LLM
decision could not honestly be folded into any of the others — it carries the PDPA argument, and
leaving it implicit would have meant a system whose central dependency was never chosen on the
record. Six were considered (adding tech stack and authentication) and cut: the team is four
students, and two thin ADRs would have diluted four substantial ones. Those remain candidates in
`docs/adr/INDEX.md`.

**ADR-001 — five services behind a gateway, discovery via Kubernetes.** Boundaries drawn by
capability, following three properties that genuinely differ: who triggers the work (a human, a
queued message, or the clock), how long it may take, and what data it owns. Rejected: the modular
monolith, one service per use case, and decomposition by technical layer. The monolith argument is
recorded honestly in the ADR's Notes — with no course constraint we would likely have started with
two deployables, and what survives the constraint is the *shape* of the split, not the count.
Consul was rejected because Kubernetes already derives the same information from readiness probes
it is running anyway; the cost is that all four members now have to learn Kubernetes, which is
recorded as the largest schedule risk the decision creates. Go is the backend language.

The service that owns UC-4 and UC-5 is named **Compliance & Insights**. The *Requirements* entry
below referred to it in passing as *Insights & Notifications*; that name was provisional, and
ADR-001 does not adopt it, because retention ownership is the service's defining responsibility
and notifications are not a service — each event's owner emits its own (FR-2.12 from Hiring,
FR-4.4 and FR-5.3 from Compliance & Insights).

**ADR-002 — one queued message per resume, on RabbitMQ.** The decision that mattered was not the
broker but the *unit of work*: per-resume rather than per-batch, so the unit of failure is the
same size as the failure. With one message per batch, a single corrupt file poisons two hundred
resumes and a retry re-pays every LLM call already made. Kafka was rejected on head-of-line
blocking — one slow resume would stall its partition, which is precisely the failure this design
exists to prevent — and a PostgreSQL job table was rejected because it would put batch contention
on the same database serving recruiter traffic, undoing ADR-001's boundary from below.

**The demonstrated quality attribute was reconsidered, and Scalability stands.** ADR-002 was
first drafted with *Reliability* as the attribute the project demonstrates — the design's most
distinctive property is that no dependency failure loses work, and UC-2's alternate flow 4a
already promised it. On rebasing onto the *Requirements* entry, which had independently chosen
Scalability (NFR-07) that morning, we kept Scalability rather than overturn a teammate's merged
decision. The reasons it was the right call, not merely the polite one: NFR-07 is easier to show
live — add workers, watch the throughput line — while a fault-injection demo is harder to make
legible in ten minutes; and nothing in the design changes either way, because the per-resume
queue is what delivers both. The reliability claim survives intact as NFR-10 and its
fault-injection test, and ADR-002 now argues that the second test is what keeps the first honest:
throughput bought by dropping work is not throughput. The reconsideration is recorded in ADR-002's
Notes.

**ADR-003 — PostgreSQL as system of record, MongoDB for AI-derived documents.** The course
requires two database types, and the ADR says plainly that the requirement forced the question —
then gives an architectural reason that stands without it. Keeping identifying material physically
separate from the counts the UC-4 dashboard is built on turns UC-5's *anonymise* action (FR-5.6)
into "drop the documents, keep the rows": coarse and provable, rather than a field-by-field
rewrite one missed column away from a compliance failure. PostgreSQL-with-`jsonb` is recorded as a
genuinely strong rejected position rather than a straw man. The accepted cost is that no
transaction spans the two stores, which is now named as the architectural cause of the *deletion
pending* state FR-5.11 already required — a case where writing the ADR explained something the
requirements had anticipated by instinct.

**ADR-004 — all model access through one AI Service, managed API only, no fallback model.** The
provider's training clause is treated as non-negotiable rather than as a preference: a provider
that trains on submitted data absorbs every resume into a weight we cannot delete from, which
would make UC-5's erasure promise false from the first batch screened. Self-hosting an open-weight
model was the position the team was most reluctant to reject — it is the strongest privacy answer —
and it lost on hardware we do not have and on materially weaker Thai-language quality, which would
have converted a privacy win into a fairness loss in the one place the product must not fail. The
AI Service is also where NFR-14's protected attributes are stripped before any prompt, so no
caller can forget to, and it is NFR-17 made structural.

**The hybrid was considered and rejected, which is worth recording because it looks like the
obvious answer.** With NFR-10 promising that no accepted resume is lost, a local fallback model
seems free. It is not: a fallback makes a candidate's score depend on which model happened to be
healthy, so two candidates in the same batch could be ranked against each other on incomparable
numbers — indefensible in a system whose proposition is a justified, comparable score. So NFR-10
is read for exactly what it says: *every resume reaches a terminal state*, not *the AI is always
available*. ADR-002 and ADR-004 are therefore a pair, and each names the other — running without a
fallback is only acceptable because no queued work is lost. The consequence is stated in both
records and belongs in the risk matrix: during a provider outage, screening produces no scores at
all.

**Aligned with the *Requirements* entry while rebasing.** ADR-002 had been drafted against the
earlier PDF-or-DOCX intake and treated an unsupported format as a permanent processing failure; it
now follows the PDF-only decision — a non-PDF file is rejected at upload (FR-2.1) and never reaches
the queue, so the permanent-failure class is only a corrupt file or a scanned image with no text
layer. NFR-02's 60-second abandon-and-retry bound is adopted as the definition of a transient
failure, and NFR-04's two-second acknowledgement as the bound on the accept path.

**Documents brought into line.** `PROPOSAL.md` gained a full *ADRs* section (summary plus the
connective tissue between the four) and moved to Draft 3, with its status line finally reflecting
that the requirements are done. `adr/INDEX.md` has the four rows, names which five of the eight
candidates from the *Requirements* entry are now answered and by which record, and lists seven
that remain — the three unanswered ones plus four the ADRs themselves surfaced: front-end
framework, the UC-0 session/token mechanism and how workspace identity travels on internal calls,
the scheduler shared by UC-4 and UC-5, and the repository structure. `CONTEXT.md` had four
decided items removed — settled things move out of CONTEXT rather than being copied — and gained
four new open *questions*, each load-bearing for a decision already made: the provider's current
data-processing terms, Thai-language scoring quality, whether recruiters accept a
progressively-filling batch, and whether the team can stand up a Kubernetes cluster early.
`README.md` lists the four records, and `course/ASSIGNMENT.md`'s status table now reads *Done* for
the ADR row.

**Still outstanding:** the use case diagram needs redrawing in proper UML for submission; the
FR/NFR lists must be inlined back into `PROPOSAL.md` before submission, as the *Requirements* entry
notes; the load-test plan and the risk matrix, both of which ADR-002 and ADR-004 now feed; and the
seven candidate decisions above.

---

## 2026-09-11 — Requirements: reconciled, extracted, and rebuilt around quality attributes

**UC-2 accepts PDF only.** Settled while reviewing draft functional requirements: DOCX was
dropped from the accepted formats, so the use case now states PDF in its description, requires
the Recruiter to upload PDFs, and has the System reject non-PDF files at validation.

Unsupported formats moved out of the *unreadable file* alternate flow, since they are now
rejected up front rather than failing during parsing. That flow keeps the two cases that
survive validation — a corrupt file, and a scanned image with no extractable text layer — so
the scanned-resume problem is still handled without needing a second file format.

**Functional requirements written up and moved out of the proposal.** They now live in
`docs/FUNCTIONAL-REQUIREMENTS.md` — 50 requirements numbered `FR-<use case>.<n>` so each traces
to the use case it serves and so adding one never renumbers the rest. `PROPOSAL.md` keeps a
pointer and a per-use-case summary table. The deferred use case D-1 has no requirements.

**Two independent drafts were reconciled.** A teammate had filled in flat-numbered `FR-01`–`FR-33`
directly in `PROPOSAL.md` while a per-use-case set was being reviewed here. The reviewed set was
taken as the base, and six requirements from the other draft were absorbed because they covered
behaviour the base set missed — five merged into existing requirements rather than added
alongside them:

- *not qualified* outcome when a must-have criterion is unmet → merged into FR-2.5
- filtering the ranked list by minimum score and by criterion → merged into FR-2.6
- collection date and lawful basis for processing → merged into FR-2.11
- retaining a generated interview guide for later retrieval → merged into FR-3.1
- erasure across every place the data is held, as a single operation leaving no partial record
  → merged into FR-5.5
- batch-completion notification → added as FR-2.12, the only one with no natural home

Two requirements from that draft were deliberately not carried over: a snapshot of the criteria
in effect when a job opening was saved, and an explicit 0–100 score range.

**Conflicts resolved in favour of the reviewed set:** PDF-only intake (the other draft allowed
DOCX), and pause/resume/close lifecycle rather than close alone.

**Staleness is measured per job opening, not per workspace.** The two drafts disagreed: a
per-job *expected time-to-fill* set by the Recruiter at creation (FR-1.6), versus a single
workspace-wide threshold in days configured by an Admin. We chose per job.

The deciding argument is that one global threshold cannot serve roles with genuinely different
hiring horizons — set low enough to catch a stalling support role and it floods alerts for a
niche senior role; set high enough for the niche role and easy roles rot unnoticed, which is
the exact problem UC-4 exists to solve. Per job also keeps the rule local: the threshold is a
field on the job opening, so Insights & Notifications can evaluate staleness from the
`JobOpeningCreated` event it already consumes, instead of reading workspace configuration owned
by another service.

The cost accepted is that the Recruiter must supply an estimate on every job opening, and a
Recruiter who does not know the answer will guess. A workspace default with a per-job override
was considered and rejected as more machinery than this project needs.

This surfaced a gap in both drafts: nothing recorded *when* an opening was opened, which
days-open depends on. Merged into FR-1.7.

**Use cases and requirements reconciled.** A full cross-check was run in both directions — every
use case behaviour against a requirement, and every requirement against use case prose — and
thirteen discrepancies were fixed in `PROPOSAL.md`. None changed what the system does; they
removed statements the requirements no longer backed, or described behaviour the requirements
had gained.

Behaviour the use cases promised but no requirement backed, now removed from the prose:

- rate limiting of failed sign-ins (UC-0)
- exporting or sharing an interview guide (UC-3) — the guide is viewed in the application
- the "resume-specific coverage is limited" fallback for a thin resume (UC-3)
- stage-to-stage conversion on the dashboard (UC-4)
- staleness alerts for an unreviewed batch and for an undecided shortlisted candidate (UC-4),
  leaving expected time-to-fill as the single rule
- the empty-state dashboard when no positions are open (UC-4)

Behaviour the requirements gained but the use cases never described, now written into the prose:

- pause, resume and close a job opening, and the date an opening was opened (UC-1). UC-4's
  alternate flow already assumed pausing existed, so this closed a dangling reference.
- marking a candidate *not qualified* when a must-have criterion is unmet, and filtering the
  ranked list by minimum score or by criterion (UC-2)
- recording the collection date and lawful basis alongside consent status (UC-2)
- notifying the Recruiter when a batch finishes processing (UC-2)
- retaining a generated interview guide for retrieval before the interview (UC-3)
- erasure reaching profile, resume file, screening results, justification text and interview
  guides as one operation leaving no partial record (UC-5)

UC-4's description was also rewritten to explain *why* the threshold is per opening rather than
per workspace, so the decision is visible to a reader who never opens the changelog.

**Non-functional requirements rewritten around quality attributes**, and moved to
`docs/NON-FUNCTIONAL-REQUIREMENTS.md`. The teammate draft had 23 grouped by loose category
(Operational, Performance, Security, Cultural and Legal, Usability); the replacement has 17
grouped by the quality attribute each serves, every one paired with how it is verified. The
brief was to keep the set small and give the architecture direction rather than aim for
completeness.

Cut, with reasons:

- *Duplicates of functional requirements* — role-based access control, workspace isolation, and
  the rule that a score is never disclosed to the candidate. All three are already FR-0.4,
  FR-0.3 and FR-2.10. A requirement stated twice eventually gets stated two different ways.
- *Not a requirement* — "shall be deployed on cloud infrastructure" is an architectural
  decision and belongs in an ADR.
- *Unverifiable* — "shall comply with the PDPA". Compliance is not testable as a statement; it
  is the combined effect of NFR-12 to NFR-14 and FR-5.1 to FR-5.12, and the document now says
  so in prose instead of pretending otherwise.
- *No architectural consequence* — the Computer Crime Act, already covered by NFR-11 and NFR-12.
- *Untestable as written* — "usable without prior training". A real goal, but with no
  measurement attached it is decoration.

Added:

- **NFR-07**, throughput rising proportionally with worker count. This is the measurement for
  the course's "demonstrate one quality attribute" requirement, and it reuses the load test
  already owed.
- **NFR-10**, no accepted resume is ever lost. Once a batch is acknowledged before processing,
  a Recruiter cannot distinguish a lost resume from a slow one, so the guarantee has to be
  explicit.
- **NFR-17**, scoring model and prompt changeable within one service boundary — the thing that
  will change most often in this system.

**Scoring latency corrected.** The first draft required parse-and-score within 5 seconds, a
figure that only makes sense for deterministic parsing. Scoring is a language-model call whose
latency is dominated by generated output — realistically 8–15 seconds, longer at the tail. The
requirement was split: parsing within 5 seconds (NFR-01), scoring within 20 seconds with a
60-second abandon-and-retry bound (NFR-02). That bound also gives FR-2.7 a definition of
"failed" that it previously lacked.

The batch figure was then checked against it rather than left to contradict it: 100 resumes at
roughly 12 seconds each is about 20 minutes sequentially, so the 10-minute target holds only
with concurrency. NFR-06 now states the worker count it assumes, which makes NFR-07 the
mechanism that delivers it instead of an unrelated claim.

**Scalability chosen as the demonstrated quality attribute**, closing an open decision in
`CONTEXT.md`. It is measurable, demonstrable in a live demo, and shares a load test with an
existing requirement.

**ADR candidates now derived from requirements.** `docs/adr/INDEX.md` lists eight, each with the
requirement that forces it, rather than the loose list of topics it held before.

**Changelog reminder automated.** A `PostToolUse` hook (`.claude/settings.json` plus
`.claude/hooks/changelog-reminder.py`) now fires whenever any file under `docs/` is modified
and injects a reminder to record the change here. Committed at project level so it applies to
the whole team, not one machine.

It decides by **file modification time**, not by inspecting the tool's arguments. The first
attempt matched the string `docs/` in the tool input, which was wrong in both directions: it
would have fired on a read such as `sed -n '1,50p' docs/PROPOSAL.md`, which mentions the path
but changes nothing, and it depended on knowing which argument of which tool carries a path.
Checking what actually changed on disk catches an edit however it was made — the Edit and Write
tools, a scripted Bash heredoc, `sed -i`, a redirect — and never fires for a read.

It stays silent when `CHANGELOG.md` was part of the same change, and remembers the last change
it reported so it does not repeat itself for the same edit.

`.gitignore` previously excluded all of `.claude/`, which would have kept the hook on one
machine. It now shares `.claude/settings.json` and `.claude/hooks/` with the team while still
ignoring `.claude/settings.local.json` and anything else personal.

**Still outstanding.** No ADRs are written yet. The use case diagram still needs redrawing in
proper UML for submission.

---

## 2026-09-10 — ADR template rebuilt, and external context brought into the repo

*(After the initial commit `b4b9a3c`.)*

**ADR template rebuilt on Jeff Tyree & Art Akerman** rather than the simpler
Context/Decision/Status/Consequences format used in the first commit. The course's ADR deck
presents several templates; we chose this one because it forces the two things the lecturer's
deliberately bad sample was missing into their own required fields — the rejected alternatives
(**Positions**) and the mapping from decision to requirement (**Related requirements**).

Three additions to the original 14 fields, documented inside the template so the deviation is
explicit:

- **Date and Deciders** header — the original carries no traceability line, and per-member Git
  contribution is graded.
- **A one-sentence Alexandrian summary** at the top. It doubles as a readiness test: if the
  sentence will not form, the decision is not clear enough to record.
- **Optional fields marked as optional.** Fourteen mandatory sections on a term project
  produces filler, and filler buries the real content. Omit rather than pad with "N/A".

Nygard's guidance on length and voice is kept — one to two pages, written as a conversation
with a future developer.

**External context brought into the repo.** `CLAUDE.md` had been pointing at
`slides/4-1-ADRs.pdf`, which lives outside the repository — a dead reference from any clone.
More seriously, the assignment brief and submission guideline existed only in email and in
class, so a future session would have had no way to know the required headings, the "at least
3 use cases" rule, the warning that terse or ambiguous use cases lose marks, or the
instruction not to add «include»/«extend» decoratively.

- `docs/course/ASSIGNMENT.md` now holds the brief and the guideline in the original Thai with
  English glosses, and a note under each clause saying what it means for our documents.
- `CLAUDE.md` gained a *Context outside this repository* section with the rule: anything that
  influences the documents must be captured inside the repo, external files are summarised
  rather than linked by path, and provenance is named so a reader does not need the source.
- The lecturer's slide decks and the syllabus stay out of the repo deliberately — they are the
  lecturer's material, and this repo holds documentation we wrote.

**Changelog policy changed.** Entries are now written once and left alone; later work gets a
new entry, even on the same day. The `2026-09-10 — Initial proposal and repository setup`
entry is frozen as the baseline covering everything up to the first commit. Previously the
rule was to append to the current day's entry, which would have meant rewriting committed
history in prose.

---

## 2026-09-10 — Initial proposal and repository setup

> **Baseline.** This entry covers everything up to the first commit. Frozen — do not edit it;
> add a new entry above instead.

**Project defined.** HireAssist — an AI-assisted hiring-support layer for companies that
receive more applications than they can screen carefully and interview well. Positioned
deliberately as an assistant layer over existing hiring channels, **not** a job board, job
application platform, or another ATS.

**Target customer defined by a ratio, not company size.** The qualifier is applications
received versus capacity to judge them properly, so a small startup and a mid-size company
with an outnumbered recruitment team are the same customer. Thai tech SMEs are the initial
beachhead, not the boundary.

**Two failure modes named as the core problem:** screening goes shallow, and interviews go
unprepared. The second was added after it became clear the interview-questions use case was
solving a problem the document never stated.

**Six use cases specified** (UC-0 to UC-5) with actors, flows, alternate flows and outcomes,
plus a use case diagram:

- UC-0 Authenticate and manage workspace access
- UC-1 Create a job opening from natural-language requirements
- UC-2 Batch-screen resumes against a job opening
- UC-3 Generate candidate-specific interview questions
- UC-4 Monitor hiring pipeline and stale positions
- UC-5 Enforce candidate data retention

**Decisions made while scoping the use cases:**

- **Rejection reasoning is not a use case.** It is a required justification output of UC-2's
  scoring and is stored on the rejection decision. Internal only — never sent automatically
  to a candidate.
- **PDPA was split.** Retention enforcement became UC-5, scheduler-driven and auditable.
  Consent gating remains a cross-cutting policy rather than a use case of its own.
- **Talent-pool re-matching deferred as D-1.** Parked, not dropped: it cannot be demonstrated
  without a deep talent pool, multiplies scoring cost across the archive, and raises
  purpose-limitation questions UC-5 must answer first. UC-2 still builds the normalised pool
  and scoring stays a separable *(profile, criteria)* behaviour so it can be reinstated
  without rework.
- **Actors reduced to Recruiter and Admin.** The separate Hiring Manager role was removed;
  Admin is a generalisation of Recruiter. Candidate is indirect, System Scheduler supporting.
- **All «include» relationships removed** from the use case diagram. Once re-matching was
  deferred, nothing shared a sub-behaviour, and factoring parts of UC-2 out purely to populate
  the diagram would have added notation without meaning. Only the conditional
  «extend» *Draft Interview Invitation Email* remains.
- **Non-customers section dropped** from Target Customers.

**Repository created.** Documentation-only for now, intended to become a monorepo once
implementation starts. Added `README.md`, `.gitignore` (which excludes `/data/` and
`/uploads/` — real candidate data must never be committed), `CLAUDE.md`, an ADR template following the
lecturer's Context/Decision/Status/Consequences sample, and an ADR index.

**Documentation split fixed.** `PROPOSAL.md` and `CONTEXT.md` had overlapping content that
went stale immediately. `PROPOSAL.md` is now the single source of truth for anything settled;
`CONTEXT.md` holds only open questions, open decisions, the glossary, and provenance.

**Still outstanding:** Functional Requirements, Non-functional Requirements, and at least
three ADRs. The use case diagram needs redrawing in proper UML notation for submission.
