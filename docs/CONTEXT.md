# HireAssist — Working Notes

> **`PROPOSAL.md` is the single source of truth** for the project name, problem, target
> customers, use cases, requirements and ADRs. Nothing settled is repeated here.
>
> This file holds only what the proposal deliberately does **not** carry: unanswered
> questions, decisions still open, and shared vocabulary for the documents that come next.
> Course constraints live in [course/REQUIREMENTS.md](course/REQUIREMENTS.md) and
> [course/GRADING.md](course/GRADING.md).

---

## 1. Open questions — to validate

Assumptions currently stated in the proposal that we have not verified:

- [ ] Application volume per opening. Proposal claims ~20–200 — is that real for a Thai tech SME?
- [ ] Where resumes arrive today: email, JobsDB/JobThai, LINE, Google Form?
- [ ] Do target companies already use an ATS, or is it genuinely spreadsheets + inbox?
- [ ] Which resume formats must we parse — PDF, DOCX, scanned images? Thai + English mix?
- [ ] Typical expected time-to-fill, to make UC-4's staleness thresholds realistic.
- [ ] Realistic default retention period for UC-5 (6 / 12 / 24 months?).
- [ ] All NFR figures are estimates — validate against measured load-test results and revise.
- [ ] Model-provider rate limits may cap concurrency before worker count does (risk matrix item).

Raised by the ADRs recorded on 2026-09-11, and load-bearing for them:

- [ ] **The chosen LLM provider's current data-processing terms.** ADR-004 rests entirely on the
      provider not training on API inputs and on a short, documented retention window. Verify
      against the provider's own terms — not against the ADR, which will age — before any real
      resume is processed.
- [ ] **Thai-language scoring quality.** ADR-004 assumes a frontier hosted model reads mixed
      Thai/English resumes well enough to score them fairly. Needs a small evaluation set of
      synthetic Thai-shaped resumes to confirm.
- [ ] **Do recruiters accept a batch that fills in progressively?** ADR-002's whole interaction
      model assumes yes, provided progress is visible. Untested.
- [ ] **Can the team run a Kubernetes cluster early?** ADR-001's service discovery depends on it.
      If not, discovery has to be reconsidered before anything else is built on top.

---

## 2. Open decisions — ADR candidates

Each of these should become an ADR, or be folded into one, before implementation begins.

- [ ] **Erasure cascade — orchestration or choreography.** The Service–Operations–Collaborators
      table assumes orchestration: Compliance & Insights calls each holder and tracks completion.
      Needs an ADR before it is built, not a table row.
- [ ] **Who owns the candidate record, and is "talent pool" still a useful term?** FR-2.11 says
      candidates are added to the talent pool, but the pool is a view — candidates that have not
      expired — not a collection anyone maintains, and the term earned its place when D-1
      re-matching was in scope. Separately, the record FR-2.11 actually creates (candidate
      identity plus collection date, the anchor retention runs on) is assigned to Resume
      Processing in the Service–Operations–Collaborators table but is not yet settled in an ADR.
      Resume Processing already owns the parsed profile, so splitting identity from profile would
      create a dual write across services. Parked 2026-09-12.
- [ ] **Resume text extraction — a library inside Resume Processing, or an external OCR
      provider?** The Service–Operations–Collaborators table draws extraction as an in-process
      library in Resume Processing, and ADR-002 treats a scanned image with no text layer as a
      permanent failure sent to manual review. An external OCR provider is the undecided
      alternative: it would be a second processor of resume data, needing the same terms check as
      the model provider (ADR-004). Parked 2026-09-12.
- [ ] **Resume ingestion channel.** Batch upload is confirmed in UC-2. Is an automated channel
      (IMAP/webhook) in scope, or a stated future extension? *(Currently unresolved — the
      proposal's positioning says "plugs into existing channels" while UC-2 is upload-only.)*
- [ ] **Tiered screening pipeline.** A cheap deterministic filter before the model, so that
      NFR-02/NFR-06/NFR-08 hold when a provider's rate limit caps concurrency before worker
      count does. ADR-002 and ADR-004 leave this open deliberately.
- [ ] **Erasure cascade — orchestration or choreography.** ADR-001 makes erasure a cross-service
      protocol and ADR-003 makes it span two stores; neither says which component drives it or
      how the verification pass works (FR-5.3, FR-5.8).
- [ ] **Scheduler design** shared by UC-4 (staleness) and UC-5 (retention). Where the timer runs,
      how it behaves with more than one replica, and how a sweep that dies half-way resumes.
- [ ] **Session and token mechanism for UC-0**, and how the role travels on internal REST calls.
      ADR-001 requires that it does, and says enforcement cannot live only at the gateway — but
      not how.
- [ ] **Repository structure** once implementation starts — monorepo layout, and where the shared
      OpenAPI definitions live.
- [x] ~~Which quality attribute we demonstrate~~ → **Scalability** (NFR-07); how it is delivered
      and measured is in ADR-002.
- [ ] ~~Re-match trigger (event vs. scheduled)~~ — moot while D-1 is deferred.

Decided on 2026-09-11 and moved out of this file — see [adr/INDEX.md](adr/INDEX.md): service
decomposition, protocols and service discovery (ADR-001), the async processing model for UC-2
(ADR-002), the datastore split (ADR-003), LLM provider and placement (ADR-004), per-service
language choice (ADR-005, Proposed), and one deployment per customer (ADR-006).

---

## 3. Requirements

Written up and moved out of this file:

- **Functional** — `FUNCTIONAL-REQUIREMENTS.md` (44, numbered by use case)
- **Non-functional** — `NON-FUNCTIONAL-REQUIREMENTS.md` (17, grouped by quality attribute)

The quality attribute demonstrated for the course requirement is **Scalability**, measured as
batch screening throughput against worker count (NFR-07).

---

## 4. Glossary

Shared vocabulary for the proposal, the architecture diagram, and the
Service–Operations–Collaborators table.

| Term | Meaning |
|---|---|
| **Member** | A person who has been given credentials to sign in to this installation, holding exactly one role: Admin or Recruiter. Signed out, a Member is a Guest; signed in, they act in their role. A removed Member can no longer sign in but still identifies the person behind their past actions. *Avoid:* account, membership; *user* only in its everyday sense of anyone using the software. |
| **Temporary Password** | A password an Admin sets for a Member, when creating them or later. It cannot start a session; the Member must first replace it with one only they know. *Avoid:* initial password. |
| **Guest** | A person who has not signed in. On signing in they act as a Recruiter or an Admin, so *Recruiter* and *Admin* always mean a signed-in user. |
| **Job Opening** | An open role, holding weighted screening criteria and an expected time-to-fill. |
| **Criterion** | One requirement of a job opening, flagged must-have or nice-to-have, with a weight. |
| **Resume** | The raw file a candidate submitted. |
| **Candidate Profile** | The normalised structure parsed from a resume: contact, history, skills, education. |
| **Candidate** | A person. May have profiles and decisions across several job openings over time. |
| **Screening Batch** | One submitted set of resumes processed asynchronously against a job opening. |
| **Screening Score** | The weighted match of a profile against criteria, always with a justification. |
| **Justification** | The written evidence-based reason accompanying a score. Internal only. |
| **Decision** | Shortlist or reject, recorded with its reason and with any human override of the AI score. |
| **Interview Guide** | The generated question set for one candidate against one job opening. |
| **Talent Pool** | All retained candidate profiles, governed by the retention policy. |
| **Stale Position** | An open position breaching its staleness rule (e.g. past expected time-to-fill). |
| **Retention Policy** | Period, anchor date (data collected, or last activity), and expiry action (delete or anonymise). |
| **PDPA** | Thailand's Personal Data Protection Act. |

---

## 5. Provenance — original ideas → current use cases

Kept only as a record of where the concept came from. The authoritative descriptions are in
`PROPOSAL.md`.

| Original idea | Became |
|---|---|
| AI Screen Resume | UC-2 |
| Reason for rejection (not sent to candidate) | An output field of UC-2, not a use case |
| Talent Pool Re-matching | **D-1 — deferred** (see *Deferred Use Cases* in the proposal) |
| PDPA Assistant | UC-5 (retention). Consent is obtained before a resume reaches HireAssist and is not stored |
| First draft email for interview meeting | Was an «extend» on UC-2; removed — no use case or requirement ever described it |
| Notification for position open too long | Folded into UC-4 |
| *(new)* Job creation from natural language | UC-1 |
| *(new)* Candidate-specific interview questions | UC-3 |
| *(new)* Authentication and access management, first drafted as one use case | Split in two after review: UC-0 (Guest signs in) and UC-6 (Admin manages members and roles) |

---

## 6. File map

| File | Purpose |
|---|---|
| `PROPOSAL.md` | **Source of truth** — the submission document |
| `CONTEXT.md` | This file — open questions, open decisions, glossary |
| `diagrams/` | Diagrams as code — PlantUML source plus the SVG rendered from it |
| `adr/INDEX.md` | ADR index; `adr/TEMPLATE.md` is the format to use |
| `course/` | Course minimum requirements & grading breakdown |
| `reference/` | The lecturer's ADR samples (good and bad) |
