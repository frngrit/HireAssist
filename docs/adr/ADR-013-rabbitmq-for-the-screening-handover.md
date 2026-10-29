# ADR-013: RabbitMQ carries the screening handover between Hiring and Resume Processing

**Date:** 2026-10-17
**Deciders:** Patiphon Puntusin, Thanabul Parodom, Thanwarat Korcharoenkiat, Rerngrit Jangsri —
agreed on 2026-10-16; built by Thanwarat Korcharoenkiat (Hiring) and Thanabul Parodom (Resume
Processing)

> In the context of batch resume screening, where each resume is minutes of unreliable model work
> handed from the service that owns the batch to the service that screens it, facing a
> requirement that some part of the system be driven by a message broker and a hand-built work
> table and callback that re-implement what a broker supplies, we decided that each resume travel
> as one message on a durable RabbitMQ queue, with its result returned on a second queue, to
> achieve the same per-resume isolation, retry and no-loss guarantee with less code and a
> pipeline that scales by adding consumers, accepting a broker the system now depends on and must
> operate, delivery that is at-least-once rather than exactly-once, and two services that must
> agree on a message contract no compiler checks.

---

## Summary

### Issue

When a Recruiter submits a batch of resumes (UC-2, batch screening), the Hiring Service owns the
batch and the Resume Processing Service does the screening: it extracts each resume's text, asks
the AI Service for a profile and a score, and stores both. Each resume is seconds to minutes of
work that depends on an external model, and some resumes fail. Earlier, ADR-002 settled how that
work is shaped, and this record keeps all of it: **one resume is one unit of work**, retried on
its own, classified as a temporary or a permanent failure, and never silently scored zero.

ADR-002 also settled how the work crosses between the two services. Every boundary was REST at
the time, so a resume was handed over by a REST call, held in a work table of Resume
Processing's own that its workers claimed rows from, and answered by a REST callback to Hiring.
ADR-002 said plainly what that cost: *we are writing by hand what a message broker would have
supplied* — the claiming, the retry schedule, the parking of what does not recover, and a
callback that must itself be retried.

Two things have changed since. The course requires at least one service driven by a message
broker, and the system had none. And the work table was the most hand-written part of the design:
the place where a broker removes the most code, rather than being added for its own sake.

### Decision

**The screening handover runs on RabbitMQ: one message per resume each way.**

1. **Hiring stores each PDF in object storage and publishes one `screening.requested` message
   per resume**, carrying the batch and resume ids, where the file is, and a snapshot of the
   opening's criteria. The batch is acknowledged at once (FR-2.2, NFR-04).
2. **Nothing accepted is lost before the broker has it.** A request is held in Hiring's
   PostgreSQL until RabbitMQ confirms it; a relay retries what is unconfirmed. A broker outage
   delays screening and loses nothing (NFR-10).
3. **Resume Processing consumes `screening.requested`**, screens the resume, and publishes one
   `screening.completed` per resume, whatever the outcome: scored, unparsable (needs manual
   review) or failed.
4. **Retries and dead-lettering live in the broker.** A temporary failure goes to a retry queue
   that returns it after a growing delay; after a bounded number of attempts, or at once for a
   permanent failure, it is dead-lettered and a `failed` result is still published, so Hiring
   never waits forever. Dead-lettered messages stay for a person to inspect and replay.
5. **Hiring consumes `screening.completed`** and records the score, the must-have check and the
   unmet must-haves. A repeated result is ignored (results are idempotent on batch and resume
   id); one that can never be accepted goes to a dead-letter queue of Hiring's own.
6. **The two shared queues are durable, take no arguments, and use the default exchange**, so
   either side may declare them. The message bodies are a written contract both owners approve.
7. **The work table is withdrawn.** Resume Processing no longer holds in-flight work in a table of
   its own, and the REST handover and callback are gone. Reading a candidate's profile and
   evidence stays a REST call.

### Status

**Accepted.** Built and demonstrated end to end on 2026-10-17.

### Group

Integration

---

## Details

### Assumptions

- One deployment serves one company (ADR-006), so one RabbitMQ instance per deployment, with no
  routing between tenants.
- Batches are tens to hundreds of resumes, not millions of events: RabbitMQ's throughput is far
  beyond the load, and the model call is the bottleneck.
- The broker runs inside the deployment's private network, like the services.

### Constraints

- The course requires REST, gRPC and a message broker; REST and gRPC were already in place.
- ADR-002's guarantees stand: per-resume isolation (FR-2.4), retry before manual review (FR-2.7),
  no accepted resume lost (NFR-10), and throughput that rises with workers (NFR-07).
- The AI Service stays behind gRPC (ADR-007); the broker does not reach it.

### Positions

1. **Keep ADR-002 as built:** REST handover, a claimable work table, a REST callback.
2. **RabbitMQ queues for the handover and the result** *(chosen)*.
3. **Kafka topics** for the same two flows.
4. **A broker for the result only**, keeping the REST handover and work table.
5. **A broker for the pipeline events** Hiring pushes to the dashboard (UC-4), leaving screening
   as it is.

### Argument

**Against keeping the work table (1).** It works and it is understood, but it leaves the course
requirement unmet, and it is the code a broker replaces: row claiming, the retry schedule, a
retried callback and a replay path, all written and tested by hand.

**Against Kafka (3).** Kafka is built for a replayable log of events read by many consumers.
Screening is a work queue: each request is taken by one worker, retried on its own, and
dead-lettered if it does not recover. Kafka has no per-message retry delay or dead-lettering of
its own, so both would be built again; and it is heavier to run for a team of four.

**Against a broker for the result only (4).** It meets the requirement in the letter, but keeps
the hand-built half — the work table — and splits one flow across two styles.

**Against the pipeline events (5).** It is where earlier documents expected the broker to go, but
UC-4 is not built, so the broker would carry nothing the demonstration can show, and the
screening work table would stay.

**For RabbitMQ on the handover (2).** Durable queues, acknowledgements, per-consumer prefetch and
dead-letter exchanges are exactly the work-queue behaviour ADR-002 built by hand. Throughput
scales by adding Resume Processing consumers, with nothing to coordinate between them.

### Implications

**What this buys us**

- The course's broker requirement is met where it does real work.
- ADR-002's guarantees are kept with less code: no claiming, no hand-built retry scheduler, no
  retried callback.
- Screening scales by running more Resume Processing replicas on the same queue (NFR-07).
- A broker outage delays screening without losing anything, because Hiring holds each request
  until the broker confirms it.
- The queues are visible: their depth and rates show the pipeline's state at a glance.

**What it costs us**

- **A new component to run and keep up.** The broker is now on the screening path; while it is
  down, screening waits.
- **At-least-once, not exactly-once.** A resume can be processed — and the model paid — twice
  after a crash or a redelivery. Results are idempotent, so the outcome is right, but the cost is
  not.
- **A contract no compiler checks.** The message bodies are JSON agreed in writing; a field
  renamed on one side breaks the other at runtime. REST has OpenAPI and the AI Service a `.proto`;
  the queues have neither.
- **Order is not guaranteed** across resumes, so results arrive in any order and the ranking
  updates as they do.
- **Two dead-letter queues to watch**, one per direction, each needing a person to inspect and
  replay.
- **Load tests now measure the broker too**, which changes what NFR-06 and NFR-07's figures
  mean.

---

## Related

### Related decisions

- [ADR-002](ADR-002-async-screening-pipeline.md) — the per-resume unit of work, its retry
  classification, idempotent results and completion rule all stand. This record overrides how the
  work crosses between the services: a queue in place of the REST handover, the work table and
  the callback.
- [ADR-001](ADR-001-service-decomposition.md) — REST on every boundary now has a second exception
  beside the AI Service: the screening handover is asynchronous messaging.
- [ADR-003](ADR-003-polyglot-persistence.md) — Hiring keeps the score in PostgreSQL; the profile,
  justification and evidence stay in Resume Processing's MongoDB, read over REST when shown.
- [ADR-006](ADR-006-single-tenant-deployment.md) — one broker per deployment.

### Related requirements

- **UC-2, batch screening**, is the use case served.
- **FR-2.2** — acknowledge a batch without waiting for it. **FR-2.4** — each resume processed
  independently. **FR-2.6** — the ranking updates as results arrive. **FR-2.7** — retry, then flag
  for manual review rather than score. **FR-2.12** — notify when a batch has finished.
- **NFR-04** — acknowledge a batch within two seconds. **NFR-06, NFR-07** — batch time and
  throughput rising with workers, the demonstrated quality attribute. **NFR-10** — no accepted
  resume lost.
- **Course requirements:** at least one service driven by a message broker.

### Related principles

- *Prefer a proven component to hand-written infrastructure* where the behaviour is the same.
- *PDPA is a design force* — a message carries ids, a file location and criteria, never the
  resume's text or the candidate's contact details.
