# ADR-011: Interview guides live in a document store belonging to the Hiring Service

**Date:** 2026-10-17
**Deciders:** Patiphon Puntusin (implementing interview guides in the Hiring Service)

> In the context of an interview guide — generated questions and the notes written against them,
> owned by the Hiring Service — facing a rule that puts AI-derived documents in a document store
> while that store so far belonged to one other service, we decided to give the Hiring Service a
> document store **of its own**, to achieve a store each service keeps private and an erasure
> that deletes documents rather than reaching inside rows, accepting a second database for one
> service in every installation, with no transaction spanning the two.

---

## Summary

### Issue

[ADR-003](ADR-003-polyglot-persistence.md) split storage by what the data is: a relational
database is the system of record — openings, criteria, scores, decisions, anything a constraint
should protect or a report should count — and a document store holds AI-derived documents, whose
shape follows the prompt that produced them. When that record was written, the only service
holding such documents was the one that parses resumes, so the document store was, in practice,
its store.

Interview guides (UC-3) break that coincidence. A guide is a set of generated questions, each
tagged with the criterion it tests and the resume evidence it came from, plus whatever the
interviewer later writes against each one. It is squarely an AI-derived document, and it is
owned by the Hiring Service, which until now had only the relational system of record.

Three forces decide where it goes.

**No service reads another service's store.** That is the boundary rule the decomposition rests
on ([ADR-001](ADR-001-service-decomposition.md)). A guide kept in the resume-parsing service's
database would have to be reached either by reading that database directly, which the rule
forbids, or through an operation on that service, which would make it the keeper of an
interview artifact it has no business knowing about.

**Erasure has to be coarse to be provable.** Candidate personal data must be deleted everywhere
it is held when retention expires (FR-5.3), within thirty days of a valid request (NFR-13). The
split in ADR-003 exists partly so that erasure can *drop the documents and keep the counts*. A
guide quotes the candidate's resume back at them, and an interview note is free text a person
typed about a named candidate, so a guide is personal data and must be erasable the same way.

**Every store is a store in every installation.** One deployment serves one company
([ADR-006](ADR-006-single-tenant-deployment.md)), so each database chosen here is a database
that every customer runs, every developer runs on a laptop, and someone backs up.

### Decision

**The Hiring Service owns a document store of its own, and interview guides live in it.**

- Guides are documents in a database belonging to the Hiring Service alone. No other service
  reads or writes it, and the Hiring Service does not read anyone else's.
- One document per candidate per job opening, with the database enforcing that: a guide is
  revised in place, so generating twice cannot produce two.
- A document holds the questions in the order they are asked, each with its criterion, its
  evidence and the interviewer's note, together with how the guide came about — the emphasis
  asked for, whether the current questions are the model's or the Recruiter's, which model
  produced them, and its revision count.
- The relational system of record keeps what it already keeps. Nothing about a guide is
  duplicated into it, and nothing joins across the two.
- Erasing a candidate's data from the Hiring Service deletes their guides, notes included.

### Status

**Accepted.** The store class was already decided by ADR-003; what was open is whose database,
which is within one service's own storage and is that service's to settle.

### Group

Data

---

## Details

### Assumptions

- A guide is small — tens of questions at most — always read whole, by one candidate and one
  opening, and never queried across candidates. Nothing about it wants a join.
- Its shape will change as the prompt changes: a new field on a question is expected, and should
  not be a migration.
- Interview notes are written after the interview, by one person at a time. There is no
  concurrent editing to resolve.
- An installation runs its databases as managed services or containers it already operates, so a
  second one is an operational cost rather than a new capability to learn.

### Constraints

- ADR-003 already decided that AI-derived documents go in a document store and the hiring record
  stays relational. This record does not reopen that; it answers whose store.
- ADR-001 forbids one service reading another's data, which rules out sharing a database with
  the service that parses resumes.
- FR-5.3 and NFR-13 require candidate personal data to be erasable everywhere it is held, which
  makes every new location a location to erase from.
- [ADR-005](ADR-005-per-service-language.md) leaves each service its own language, so "a client
  library exists" is a condition on the store, per language, rather than a given.

### Positions

1. **A document store belonging to the Hiring Service** *(chosen)*.
2. **A JSON column in the Hiring Service's relational database**, alongside the opening.
3. **The resume-parsing service's document store**, reached through an operation it exposes.
4. **One document database shared by both services**, a collection each.
5. **Object storage**, one serialised guide per object.

### Argument

**Against a JSON column (2).** This is the close one. A guide is small and always read whole,
the Hiring Service already runs a relational database, and a JSON column would add no
infrastructure at all — no second client, no second thing to back up, no second thing to start
before a demo. Two things decide against it. The first is erasure: ADR-003 draws its line so
that removing a candidate is dropping documents rather than editing rows of the record, and a
guide inside the hiring tables makes a person's data something to reach into rows for, in a
table whose other columns must survive. The second is that the rule stops being a rule. If
AI-derived documents go in the document store *except when they are small*, then the next one —
a cached proposal, a stored justification — is argued again from scratch, and the storage split
becomes a matter of taste rather than something a reader can predict.

**Against the resume-parsing service's store (3).** It would make that service the keeper of
interview material it has no responsibility for, and either break the rule that no service reads
another's database or give it an operation about interviews. The boundary between the two
services is capability, not storage technology; sharing a database because the technology
matches is how a boundary quietly stops existing.

**Against one shared document database (4).** Two services writing into one database is the
coupling that service boundaries exist to prevent: an index, a migration or a load problem
caused by one is felt by the other, and the temptation to read the other collection is one query
away.

**Against object storage (5).** It cannot find a guide by candidate and opening without a second
index somewhere, and guides change — a note is written against a question after the interview —
while object storage suits the immutable things, such as the resume file itself.

**For the service's own document store (1).** It applies the rule ADR-003 already set without
weakening it, keeps each service's data private as ADR-001 requires, lets a guide's shape follow
the prompt, and makes erasing a candidate a deletion of documents keyed by that candidate.

### Implications

**What this buys us**

- The storage rule stays predictable: AI-derived documents go to a document store, and the next
  one needs no new argument.
- Each service's store stays private, so the boundary holds in storage as well as in operations.
- A guide's shape can change with the prompt without a migration.
- Erasure is a deletion of documents, and what it deleted is easy to state.

**What it costs us**

- **The Hiring Service now runs two databases**, and so does every installation of the product,
  every developer's laptop and every demo. One service, two stores to start, back up, upgrade
  and connect to.
- **There is no transaction across them.** A guide and the relational record it belongs to are
  written separately, so a failure between the two writes leaves them disagreeing. Today only
  one operation touches both, and the direction of the failure is a guide without its effect
  rather than the reverse; anything that later needs both to move together needs a reconciling
  step, not a transaction.
- **Candidate personal data now sits in two services' document stores**, not one. The erasure
  path has one more place to reach, and the thing it deletes includes interview notes — free
  text one person wrote about another, which is the most sensitive content the system holds.
- **A listing that needs both stores is two reads.** "Which shortlisted candidates already have
  a guide" cannot be one query.
- **The service needs a client for the document store in its language**, maintained, in addition
  to its relational one — a condition on any language the service might be rewritten in.
- **Interview notes are not AI-derived**, and they live here because they belong to the question
  they answer. That is a deliberate exception to a rule stated in terms of what produced the
  data, and it is worth naming so that it is not read as the rule changing.

---

## Related

### Related decisions

- [ADR-001](ADR-001-service-decomposition.md) — capability boundaries and the rule that no
  service reads another's data. This record keeps that true for a document type that two
  services could plausibly have shared.
- [ADR-003](ADR-003-polyglot-persistence.md) — the relational-versus-document split being
  applied. Unchanged: this record answers only which service's document store, a question that
  record did not need to ask when one service held all the documents.
- [ADR-005](ADR-005-per-service-language.md) *(Proposed)* — per-service languages; a maintained
  client for this store becomes one of the conditions a language must meet for this service.
- [ADR-006](ADR-006-single-tenant-deployment.md) — one deployment per company, which is what
  turns one extra database into one extra database per customer.
- [ADR-008](ADR-008-profile-extraction-is-a-model-call.md) — the candidate profile is an
  AI-derived document and is stored as one. Interview guides are treated the same way, in a
  different service's store.

### Related requirements

- **FR-3.1**, generating an interview guide from the candidate's profile and the opening's
  criteria and retaining it so it can be retrieved before the interview, is what needs a store
  at all.
- **FR-3.3**, tagging each question with the criterion it tests and the evidence it came from,
  is why a question is a structure rather than a string.
- **FR-3.4**, allowing questions to be added, edited, removed or regenerated, is why the
  document is replaced as a whole and carries a revision.
- **FR-3.5**, recording interview notes against each question, is why the document is written to
  after the interview and not only generated.
- **FR-5.3**, deleting a candidate's personal data everywhere it is held on expiry, now includes
  this store.
- **NFR-13**, completing a valid erasure request within thirty days, is why the deletion is a
  keyed document delete rather than a search through free text.

### Related artifacts

The service operations table, where the Hiring Service's ownership line now includes interview
guides; the runtime view, which names each service's stores; and the deployment configuration
for every installation, which gains a document database.

### Related principles

- *The human still decides* — the interviewer's notes are kept beside the question they answer,
  so the evidence stays attached to the criterion it was meant to test.
- *PDPA is a design force, not a feature* — the store was chosen partly for how cleanly a
  candidate can be removed from it.

---

## Notes

The alternative that nearly won was the JSON column, and it would have worked. If this store ever
looks like overhead rather than a boundary — one collection, a few hundred documents per
installation, two databases to keep alive for it — that is the decision to reopen, with the
erasure argument as the thing a replacement has to answer.
