# Service–Operations–Collaborators

HireAssist · service decomposition per **ADR-001**, as amended by **ADR-006** — 5 services + 1 API
Gateway. **Every collaboration is REST over HTTP/JSON** (ADR-001) **except calls into the AI
Service, which are gRPC** (ADR-007). There is no message broker. Only the gRPC calls are marked.

| Service | Operations | Collaborators |
|---|---|---|
| **Identity Service**<br><br>*owns: members, their roles<br>and sessions*<br><br>UC-0 · UC-6 | `authenticateMember()`<br>`validateSession()`<br>`listMembers()`<br>`getMember()`<br>`createMember()`<br>`removeMember()`<br>`changeMemberRole()`<br>`setMemberPassword()`<br>`changePassword()` | *(none)* |
| **Hiring Service**<br><br>*owns: job openings, criteria,<br>screening batches and entry status,<br>scores and overrides, shortlist<br>decisions, interview guides*<br><br>UC-1 · UC-2 · UC-3 | `proposeJobOpening()`<br>`createJobOpening()`<br>`reviseScreeningCriteria()`<br>`getScreeningCriteria()`<br>`getJobOpening()`<br>`listJobOpenings()`<br>`pauseOpening()`<br>`resumeOpening()`<br>`closeOpening()`<br>`submitScreeningBatch()`<br>`getBatchStatus()`<br>`getRankedResults()`<br>`getScreeningResult()`<br>`overrideScore()`<br>`shortlistCandidate()`<br>`generateInterviewGuide()`<br>`reviseGuide()`<br>`getInterviewGuide()`<br>`recordInterviewNote()`<br>`reportScreeningResult()`<br>`eraseHiringData()` | **Object Storage Adapter**<br>　`storeResumeFile()`<br>**Resume Processing Service**<br>　`submitResumeForScreening()`<br>　　*(carries the criteria snapshot)*<br>　`getCandidateProfile()`<br>**AI Service** *(gRPC)*<br>　`deriveCriteriaFromDescription()`<br>　`generateInterviewQuestions()`<br>**Compliance & Insights Service**<br>　`recordPipelineEvent()`<br>**Email Adapter**<br>　`sendEmail()` |
| **Resume Processing Service**<br><br>*owns: candidate identity and<br>collection date, candidate profiles,<br>parsed resume text, per-criterion<br>scoring output and justifications*<br><br>UC-2 (per-resume work) | `submitResumeForScreening()`<br>`getCandidateProfile()`<br>`listCandidateProfiles()`<br>`listUnparsableResumes()`<br>`eraseCandidateProfile()` | **Object Storage Adapter**<br>　`fetchResumeFile()`<br>**AI Service** *(gRPC)*<br>　`extractProfileFromText()`<br>　`scoreAgainstCriteria()`<br>**Hiring Service**<br>　`reportScreeningResult()` *(callback)* |
| **AI Service**<br><br>*owns: prompts and model credentials<br>— no domain data*<br><br>UC-1 · UC-2 · UC-3 | `deriveCriteriaFromDescription()`<br>`extractProfileFromText()`<br>`scoreAgainstCriteria()`<br>`generateInterviewQuestions()` | **LLM Adapter**<br>　`complete()` |
| **Compliance & Insights Service**<br><br>*owns: retention policy, pipeline<br>metrics, erasure and access audit logs*<br><br>UC-4 · UC-5 | `setRetentionPolicy()`<br>`evaluateRetention()`<br>`listHeldExpiries()`<br>`resolveHeldExpiry()`<br>`eraseCandidate()`<br>`getErasureAudit()`<br>`getPipelineDashboard()`<br>`evaluateStaleness()`<br>`recordPipelineEvent()`<br>`recordAuthorisationRejected()` | **Hiring Service**<br>　`eraseHiringData()`<br>**Resume Processing Service**<br>　`eraseCandidateProfile()`<br>　`listCandidateProfiles()`<br>**Email Adapter**<br>　`sendEmail()` |

**API Gateway** is the entry point, not a domain service. It terminates TLS, routes, rate-limits,
and resolves the caller's role from the access token, whose signature and expiry it verifies
itself against the Identity Service's public key rather than by calling the Identity Service on
every request (**ADR-009**, now implemented at the gateway). It rejects any request exceeding the
caller's role and calls `Compliance & Insights Service.recordAuthorisationRejected()` for the
access audit log. For the web application it keeps the refresh token in an HttpOnly cookie, and
serves the sign-out that clears it (**ADR-010**). It owns no data.

It is also the **only** place a token is checked: the services behind it do not verify what they
receive, because the deployment keeps them on a private network. That decision is not recorded
in an ADR yet, and it is what makes publishing anything but the gateway unsafe.

**External systems**, each behind an adapter: Object / Blob Storage, LLM Provider (reached only by
AI Service), Email Provider. PDF text extraction is an in-process library in Resume Processing,
not an external service — a scanned image with no text layer is a permanent failure by design
(FR-2.7), not something to send to an OCR provider.

No service reads another service's data store. Every crossing is one of the operations above —
REST, or gRPC where marked.

---

## Notes on five collaborations

**Hiring owns the criteria; the snapshot travels with the handover.** Criteria belong to the Job
Opening, created in UC-1 and held by Hiring. `submitResumeForScreening()` carries a snapshot of
them in its body, and Resume Processing stores it on the work row for the life of that entry —
it never calls back to fetch criteria, and it is not their owner. Two reasons: a fetch per resume
puts a synchronous dependency on Hiring in the middle of the path built to have none, and
criteria can be edited while a batch runs — a snapshot pins one version to one resume, so every
justification is defensible against the criteria actually used (NFR-15).

**Screening is asynchronous even though the call is REST.** `submitResumeForScreening()` returns
as soon as the work row is durably written, not when the resume is scored; Resume Processing
calls `reportScreeningResult()` back when it has an answer. The queue is a table inside Resume
Processing (ADR-002), not infrastructure between the two services.

**Hiring reads the profile, it does not copy it.** Two main flows need candidate data Hiring does
not own: generating an interview guide (UC-3) needs the profile, and opening a screened candidate
needs the written justification. Both are AI-derived documents in Resume Processing's store, so
Hiring calls `getCandidateProfile()` for them at read time.

The alternative — carrying the justification back inside `reportScreeningResult()` and storing it
in Hiring — was rejected. It would put an AI-derived document in the relational system of record,
which is exactly the boundary ADR-003 draws to keep anonymisation coarse: *drop the documents,
keep the counts*. A justification duplicated into Hiring is a second place a person's data must be
erased from, and a second copy that can disagree with the first. The ranked list itself is
unaffected either way — score and must-have check are Hiring's own columns, so the list never
waits on a document read (NFR-03). Only opening an individual candidate does.

**Profile extraction is a model call; text extraction is not.** Resume Processing turns the PDF
into text in process — no model ever receives a file — and then asks the AI Service to structure
that text into a normalised profile before asking it to score the profile. ADR-008 records why:
resume layout is chosen by the candidate, so a rule-based parser mis-associates silently, and a
plausible wrong profile is scored with confidence. The profile is extracted once per resume and
stored; scoring happens once per resume per job opening.

**The dashboard is a projection, not a query.** Compliance & Insights maintains its own view of
open positions from the `recordPipelineEvent()` calls Hiring makes as things happen, and does not
call `getJobOpening()` to build it. ADR-001 records that metrics span services and must be
maintained incrementally.

## Open decisions this table touches

Recorded here so they are not settled by implementation:

- **Erasure is orchestrated.** Compliance & Insights calls `eraseHiringData()` and
  `eraseCandidateProfile()` and tracks completion. That is a real answer to the erasure-cascade
  question — orchestration over choreography — and it needs an ADR before it is built.
- **The candidate register sits in Resume Processing**, alongside the profile, rather than being
  split from it. Also unrecorded.
- **Pipeline events are pushed, not published.** With no broker, Hiring calls
  `recordPipelineEvent()` directly, which makes Compliance a synchronous dependency of Hiring's
  write path and gives a lost call no retry story. Needs an ADR before it is built.
- **Interview guides are owned by Hiring but are AI-derived documents.** ADR-003 places such
  documents in MongoDB, which is otherwise Resume Processing's store, so Hiring needs a document
  store of its own. Implementation has taken that path — Hiring now has its own MongoDB
  database for guides — and the decision is still unrecorded.
