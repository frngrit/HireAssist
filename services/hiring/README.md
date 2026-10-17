# Hiring Service

The recruiter-facing hiring record, in three slices:

- **Job openings (UC-1)** — `proposeJobOpening()`, which asks the AI Service to derive criteria
  from a free-text description, and `createJobOpening()`, `getJobOpening()`, listing,
  `reviseScreeningCriteria()`, `pauseOpening()`, `resumeOpening()`, `closeOpening()`.
- **Screening (UC-2)** — `submitScreeningBatch()`, `getBatchStatus()`, `getRankedResults()`,
  `getScreeningResult()`, `overrideScore()`, `shortlistCandidate()`. Each resume is handed to the
  Resume Processing Service through RabbitMQ, and its result comes back the same way.
- **Interview guides (UC-3)** — `generateInterviewGuide()`, `reviseGuide()`,
  `getInterviewGuide()`, `recordInterviewNote()`.

| Store or peer | What for | Reached through |
|---|---|---|
| PostgreSQL | job openings, criteria, batches and their criteria snapshots, scores, overrides, shortlists (ADR-003) | `pgx` |
| MongoDB | interview guides, an AI-derived document (ADR-003) | the MongoDB driver |
| Object storage (MinIO) | the resume files | `minio-go`, S3 API |
| RabbitMQ | the screening handover to and from Resume Processing | `amqp091-go` |
| AI Service | proposals (UC-1) and interview questions (UC-3) | gRPC, client generated from `/proto` (ADR-007) |
| Resume Processing Service | the justification of a score (UC-2) and the profile a guide is built from (UC-3) | REST, `getCandidateProfile()` |

Go 1.25. `kin-openapi` is used only by the contract test.

## Running

From the repository root, `docker compose up --build` starts this service on `localhost:8080`
with everything it talks to: PostgreSQL, MongoDB, RabbitMQ, MinIO, the Resume Processing Service
and the AI Service, the last on scripted answers labelled `canned`. If a port is taken, move it —
`POSTGRES_PORT`, `MONGO_PORT`, `RABBITMQ_PORT`, `MINIO_PORT` and the rest are in
`docker-compose.yml`. The same command also starts the Identity Service, the gateway and the
web app on http://localhost:4173.

| Variable | Meaning |
|---|---|
| `PORT` | HTTP port, default `8080` |
| `DATABASE_URL` | PostgreSQL connection URL. **Unset means in-memory storage**, lost on restart: fine for a quick `go run`, never for anything you want to keep |
| `MONGO_URL` | MongoDB connection URL for interview guides. Unset means in memory, with the same caveat |
| `MONGO_DATABASE` | The database within it, default `hiring` |
| `RABBITMQ_URL` | The broker. Unset means batches are accepted, but no resume is screened |
| `OBJECT_STORE_ENDPOINT`, `OBJECT_STORE_ACCESS_KEY`, `OBJECT_STORE_SECRET_KEY`, `OBJECT_STORE_BUCKET` (default `resumes`), `OBJECT_STORE_USE_SSL` | Object storage for resume files. No endpoint means files are kept in memory |
| `RESUME_PROCESSING_URL` | Where candidate profiles are read from, e.g. `http://localhost:3000`. Unset means UC-3 answers 502 and a screened candidate opens without the justification |
| `AI_SERVICE_ADDRESS` | The AI Service's gRPC address, e.g. `localhost:50051`. Unset means proposing and generating a guide answer 502 |

No peer is required to start: the service serves its own operations without them, and an
operation that needs one that is missing says so — 502 — rather than the whole service refusing
to run. Startup logs say what is missing. On startup the service connects and applies
`internal/adapter/postgres/schema.sql`; if the database cannot be reached, startup fails rather
than the first request. Every statement in the schema is idempotent (`CREATE … IF NOT EXISTS`) —
a deliberate stop-gap until the first incompatible change needs a migration tool.

## Screening, end to end

1. `POST /job-openings/{id}/batches` takes the PDFs as `multipart/form-data`, one per `resumes`
   field. It stores each file, records the batch with a snapshot of the opening's criteria, and
   answers **202** at once (FR-2.2). Files that are not PDFs are left out and listed under
   `rejected`.
2. Each resume becomes one `screening.requested` message. A resume is marked requested only once
   the broker confirms it; until then it waits in PostgreSQL, and a relay retries every 5
   seconds — so a broker outage delays screening but loses nothing (NFR-10).
3. Resume Processing answers each with `screening.completed`: scored, unparsable or failed. The
   service records the score, the must-have check and the must-haves that were unmet, and the
   resume appears in `GET /job-openings/{id}/results`. A repeated answer is ignored; one that can
   never be accepted is moved to `screening.completed.dlq`, with the reason in a header.
4. Opening one candidate (`getScreeningResult()`) reads the written justification and the
   evidence behind each verdict from Resume Processing. This service keeps no copy of them
   (ADR-003), so the ranked list never waits on another service (NFR-03).

The messages, queues and guarantees are the contract with Resume Processing,
**[`services/resume-processing/MESSAGES.md`](../resume-processing/MESSAGES.md)**. The RabbitMQ
management UI, at http://localhost:15672 (`hireassist` / `hireassist`), shows the queues; MinIO's
console, at http://localhost:9001 (`hireassist` / `hireassist-local`), shows the stored files.

When Resume Processing is not running, `go run ./cmd/fake-screener` stands in for it, answering
every request with an invented result — and `unparsable` for a file that contains the word
`scan`. It reads the same queue the real service does, so never run the two together.
Development only: its scores come from a hash, not from a model.

## Testing

```sh
go test -race ./...          # unit, contract and in-memory tests; integration tests are skipped

# with `docker compose up -d postgres mongo rabbitmq minio` running, include the integration tests:
make test-all
```

`make test-all` sets `HIRING_TEST_DATABASE_URL`, `HIRING_TEST_MONGO_URL`,
`HIRING_TEST_RABBITMQ_URL` and `HIRING_TEST_S3_*` to the compose stack. The database tests use
their own databases — `hiring_test` in PostgreSQL, which compose creates beside `hiring`, and
`hiring_test` in MongoDB — never the ones a running service writes to; the PostgreSQL tests
truncate their tables. The RabbitMQ test declares queues of its own and deletes them, so it runs
safely beside a running stack.

## Layout: clean architecture, dependencies point inward

```
api/openapi.yaml       the REST contract — authoritative (ADR-005 rule 1); location provisional
cmd/hiring/            main: reads the environment, starts and gracefully stops the server
cmd/fake-screener/     development stand-in for Resume Processing
internal/app/          composition root — the only place concrete adapters are chosen and injected
internal/adapter/
  httpapi/             REST adapter: HTTP ⇄ use case, business errors ⇄ status codes
  postgres/            the system of record + schema.sql
  mongo/               MongoDB InterviewGuideRepository — AI-derived documents (ADR-003)
  memory/              in-memory repositories and file store, used when a store is not configured
  rabbitmq/            the broker: publishes requests, consumes results, reconnects by itself
  objectstore/         resume files in S3-compatible storage
  resumeproc/          REST client for Resume Processing's getCandidateProfile(): profiles and justifications
  aigrpc/              gRPC client for the AI Service, and the stubs generated from the .proto
  system/              wall Clock (UTC, microseconds — what PostgreSQL stores) and random ids
internal/usecase/      application operations, and the ports they need
internal/domain/       job openings, screening batches, interview guides and their rules — no dependencies
```

The use cases depend only on interfaces they declare themselves. `internal/app` picks each
adapter from the environment, and nothing else knows which one it got.

## HTTP API

The contract is [`api/openapi.yaml`](api/openapi.yaml). With the service running, browse and try
it in Swagger UI at **`/docs`**; the raw spec is served at `/openapi.yaml`. Every path is under
`/job-openings`, the one prefix the gateway routes here.

**Change the spec first.** `TestHandlerMatchesOpenAPIContract` sends a request to every
operation through the real handler and validates each request and response against the spec. An
undocumented status code, a field the spec does not declare, or a spec operation the server does
not serve all fail it.

| Operation | Request | Success |
|---|---|---|
| `proposeJobOpening()` | `POST /job-openings/propose` `{"description": "…", "language": "th"}` | 200, nothing stored |
| `createJobOpening()` | `POST /job-openings` | 201 + `Location` |
| `getJobOpening()` | `GET /job-openings/{id}` | 200 |
| `listJobOpenings()` | `GET /job-openings` | 200, array |
| `reviseScreeningCriteria()` | `PUT /job-openings/{id}/criteria` (array of criteria) | 200 |
| `pauseOpening()` / `resumeOpening()` | `POST /job-openings/{id}/pause` · `/resume` | 200 |
| `closeOpening()` | `POST /job-openings/{id}/close` `{"outcome": "filled" \| "cancelled"}` | 200 |
| `submitScreeningBatch()` | `POST /job-openings/{id}/batches`, multipart, files in `resumes` | 202 + `Location` |
| `getBatchStatus()` | `GET /job-openings/{id}/batches/{batchId}` | 200 |
| `getRankedResults()` | `GET /job-openings/{id}/results?min_score=&criterion=` | 200 |
| `getScreeningResult()` | `GET /job-openings/{id}/results/{resumeId}` — with the justification | 200 |
| `overrideScore()` | `POST /job-openings/{id}/results/{resumeId}/override` `{"score": 85}` | 200 |
| `shortlistCandidate()` | `POST /job-openings/{id}/results/{resumeId}/shortlist` | 200 |
| `generateInterviewGuide()` | `POST /job-openings/{id}/candidates/{candidateId}/interview-guide` | 201 |
| `getInterviewGuide()` | `GET /job-openings/{id}/candidates/{candidateId}/interview-guide` | 200 |
| `reviseGuide()`, asking again | `POST …/interview-guide/regenerate` `{"emphasis": "more system design"}` | 200 |
| `reviseGuide()`, the Recruiter's own | `PUT …/interview-guide/questions` (the whole set) | 200 |
| `recordInterviewNote()` | `PUT …/interview-guide/questions/{questionId}/note` `{"note": "…"}` | 200 |
| health | `GET /healthz` | 200 |

```json
{
  "title": "Backend Engineer",
  "description": "Backend engineer, 3+ years, Go or Python, must have shipped with Kafka.",
  "criteria": [{"name": "3+ years of Go or Python", "classification": "must-have", "weight": 5}],
  "expected_time_to_fill_days": 45
}
```

Errors are `{"error": "..."}`. 400 means a malformed request, 404 an unknown id, 422 a broken
business rule (no criteria, a bad weight, a score off the scale, nothing in a batch that can be
screened, a guide with no questions), 409 an action the current state forbids (FR-1.9: a closed
opening is final; only a screened resume is overridden or shortlisted, once; a candidate who
already has a guide), 502 a service this one depends on that could not answer — the AI Service,
Resume Processing or object storage — and 500 anything unexpected, with no detail returned.

The ranking puts candidates who meet every must-have first, then orders by score: a must-have is
a gate, not a weight (FR-2.5). Filtering by `criterion` works on must-haves, because screening
reports which must-haves a resume missed and judges the nice-to-haves only through the score.

## Interview guides (UC-3)

The guide endpoints sit under the job opening because that is what a guide is for — one
candidate against one opening — which also keeps every hiring path behind the gateway's single
`/job-openings` route.

Generating reads the candidate's profile (`getCandidateProfile()`) and asks the AI Service for
questions covering resume claims, must-have gaps and role depth (FR-3.2). Each question comes
back tagged with the criterion it tests and the evidence it was derived from (FR-3.3):

```json
{
  "id": "ig_9c1f0b7e2d3a4b5c6d7e8f9a0b1c2d3e",
  "job_opening_id": "jo_3f9c…", "candidate_id": "cand-001",
  "questions": [
    {"text": "You list Kafka on the Wongnai project — what was the partitioning strategy?",
     "criterion": "Has shipped with Kafka", "evidence": "Kafka on the Wongnai project",
     "kind": "resume-claim"}
  ],
  "emphasis": "more system design",
  "source": "generated",
  "attribution": {"provider": "anthropic", "model": "…", "prompt_version": "uc3-1"},
  "revision": 1
}
```

Things worth knowing:

- **A candidate has one guide per opening.** Generating a second time is 409; asking the model
  again is `regenerate`, so a stray click cannot throw away what the Recruiter has edited.
- **An edited guide carries no model attribution.** The questions are partly the Recruiter's,
  and naming a model as their author would be false.
- **Notes belong to the question they answer.** After the interview, `recordInterviewNote()`
  keeps what was said with the question it answers (FR-3.5), so the evidence stays attached to
  the criterion that question was meant to test. A note does not move the revision — the
  questions have not changed — and it survives an edit that leaves its question as it was.
  Regenerating drops the notes, because the new questions are not the ones that were asked.
- **The profile is read, never copied.** The skills, the work history, the education and the
  leftover resume text are taken; the candidate's name and email address are not. A guide is
  built from what someone can be asked about, and a copy here would be a second place their
  data must be erased from. A profile created by hand has no structured fields, so its whole
  resume text is used instead.

### Regenerating the AI Service client

The stubs under `internal/adapter/aigrpc/gen/` are generated from `proto/`, like the AI
Service's own. With Docker and no local toolchain:

```sh
docker run --rm -v "$PWD/../..":/src -w /src golang:1.25-alpine sh -c '
  apk add --no-cache protobuf &&
  go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.6 &&
  go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1 &&
  PATH=$PATH:/go/bin protoc -Iproto \
    --go_out=services/hiring/internal/adapter/aigrpc/gen --go_opt=paths=source_relative \
    --go-grpc_out=services/hiring/internal/adapter/aigrpc/gen --go-grpc_opt=paths=source_relative \
    proto/hireassist/ai/v1/ai_service.proto'
```

## Not built yet

- **UC-3's precondition.** The use case says a guide is for a *shortlisted* candidate.
  Shortlisting is built now; generating a guide does not check it yet.
- **The batch-finished notification** (FR-2.12) and **`recordPipelineEvent()`** — neither is in
  this round's scope.
- **Role enforcement** (FR-0.4) — the gateway checks the caller; this service trusts it.
- **A readiness endpoint** that checks the databases (ADR-005 rule 4 asks for one; only
  `/healthz` exists).
- **A migration tool.** The schema is applied idempotently at startup until the first change that
  cannot be expressed that way.
- Clients generated from the spec — there is no consumer yet.
