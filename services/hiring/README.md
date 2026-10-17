# Hiring Service

The recruiter-facing hiring record. Two slices so far:

- **Job openings (UC-1)** — `createJobOpening()`, `getJobOpening()`, listing,
  `reviseScreeningCriteria()`, `pauseOpening()`, `resumeOpening()`, `closeOpening()`. Stored in
  PostgreSQL (ADR-003).
- **Interview guides (UC-3)** — `generateInterviewGuide()`, `reviseGuide()`,
  `getInterviewGuide()`. Stored in MongoDB, because a guide is an AI-derived document
  (ADR-003). Generating one reads the candidate's profile from the Resume Processing Service
  over REST and asks the AI Service for the questions over gRPC (ADR-007).

Go 1.25; `pgx` for PostgreSQL, the MongoDB driver, `grpc` and `protobuf` for the AI Service
client, and `kin-openapi`, used only by the contract test.

## Running

From the repository root, `docker compose up --build` starts PostgreSQL and the service on
`localhost:8080`. If port 5432 is already taken on your machine, set `POSTGRES_PORT` to another
port, for example `POSTGRES_PORT=55432 docker compose up --build`.

| Variable | Meaning |
|---|---|
| `PORT` | HTTP port, default `8080` |
| `DATABASE_URL` | PostgreSQL connection URL. **Unset means in-memory storage**, lost on restart: fine for a quick `go run`, never for anything you want to keep |
| `MONGO_URL` | MongoDB connection URL for interview guides. Unset means in memory, with the same caveat |
| `MONGO_DATABASE` | The database within it, default `hiring` |
| `RESUME_PROCESSING_URL` | Where candidate profiles are read from, e.g. `http://localhost:3000`. Unset and UC-3 answers 502 |
| `AI_SERVICE_ADDRESS` | The AI Service's gRPC address, e.g. `localhost:50051`. Unset and UC-3 answers 502 |

Neither service UC-3 reads is required to start: the Hiring Service serves its own operations
without them, and generating a guide says 502 while one is missing rather than the whole
service refusing to run.

On startup, the service connects and applies `internal/adapter/postgres/schema.sql`. If the
database cannot be reached, startup fails rather than the first request. Every statement in the
schema is idempotent (`CREATE … IF NOT EXISTS`). That is a deliberate stop-gap. Its first
incompatible change needs a real migration tool, which has not been chosen yet.

## Testing

```sh
go test -race ./...          # unit + contract tests; the database tests are skipped

# with the compose databases running, include them:
HIRING_TEST_DATABASE_URL='postgres://hireassist:hireassist@localhost:5432/hiring_test?sslmode=disable' \
HIRING_TEST_MONGO_URL='mongodb://localhost:27017' \
  go test -race ./...
```

The database tests use their own databases — `hiring_test`, which compose creates beside
`hiring`, and the `hiring_test` MongoDB database — never the ones a running service writes to.
The PostgreSQL tests truncate their tables.

## Layout: clean architecture, dependencies point inward

```
api/openapi.yaml       the REST contract — authoritative (ADR-005 rule 1); location provisional
cmd/hiring/            main: reads the environment, starts and gracefully stops the server
internal/app/          composition root — the only place concrete adapters are chosen and injected
internal/adapter/
  httpapi/             REST adapter: HTTP ⇄ use case, business errors ⇄ status codes
  postgres/            PostgreSQL JobOpeningRepository + schema.sql — the system of record
  mongo/               MongoDB InterviewGuideRepository — AI-derived documents (ADR-003)
  memory/              in-memory repositories, used when a store's URL is unset
  resumeproc/          REST client for the Resume Processing Service's getCandidateProfile()
  aigrpc/              gRPC client for the AI Service, and the stubs generated from the .proto
  system/              wall Clock (UTC, microseconds — what PostgreSQL stores) and random ids
internal/usecase/      application operations, and the ports they need (repositories, clock, ids,
                       the two services UC-3 reads)
internal/domain/       JobOpening, Criterion, InterviewGuide and their rules — no dependencies
```

The use case depends only on interfaces it declares itself. `internal/app` picks PostgreSQL or
memory from `DATABASE_URL`, and nothing else knows which one it got. A job opening is one row in
`job_openings`, and each of its criteria is one row in `job_opening_criteria`, kept in order.
`Save` replaces the opening and its criteria in one transaction.

## HTTP API

The contract is [`api/openapi.yaml`](api/openapi.yaml). With the service running, browse and try
it in Swagger UI at **`/docs`**; the raw spec is served at `/openapi.yaml`.

**Change the spec first.** `TestHandlerMatchesOpenAPIContract` sends a request to every
operation through the real handler and validates each request and response against the spec. An
undocumented status code, a field the spec does not declare, or a spec operation the server does
not serve all fail it.

| Operation | Request | Success |
|---|---|---|
| `createJobOpening()` | `POST /job-openings` | 201 + `Location` |
| `getJobOpening()` | `GET /job-openings/{id}` | 200 |
| list | `GET /job-openings` | 200, array |
| `reviseScreeningCriteria()` | `PUT /job-openings/{id}/criteria` (array of criteria) | 200 |
| `pauseOpening()` / `resumeOpening()` | `POST /job-openings/{id}/pause` · `/resume` | 200 |
| `closeOpening()` | `POST /job-openings/{id}/close` `{"outcome": "filled" \| "cancelled"}` | 200 |
| health | `GET /healthz` | 200 |
| `generateInterviewGuide()` | `POST /job-openings/{id}/candidates/{candidateId}/interview-guide` | 201 |
| `getInterviewGuide()` | `GET /job-openings/{id}/candidates/{candidateId}/interview-guide` | 200 |
| `reviseGuide()`, asking again | `POST …/interview-guide/regenerate` `{"emphasis": "more system design"}` | 200 |
| `reviseGuide()`, the Recruiter's own | `PUT …/interview-guide/questions` (the whole set) | 200 |
| `recordInterviewNote()` | `PUT …/interview-guide/questions/{questionId}/note` `{"note": "…"}` | 200 |

```json
{
  "title": "Backend Engineer",
  "description": "Backend engineer, 3+ years, Go or Python, must have shipped with Kafka.",
  "criteria": [{"name": "3+ years of Go or Python", "classification": "must-have", "weight": 5}],
  "expected_time_to_fill_days": 45
}
```

Errors are `{"error": "..."}`. 400 means a malformed body, 404 an unknown id, 422 a broken
business rule (no criteria, bad weight, unknown outcome, a guide with no questions), 409 an
action the opening's status forbids (FR-1.9: a closed opening is final) or a candidate who
already has a guide, 502 a service UC-3 reads that could not answer, and 500 anything
unexpected, with no detail returned.

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

Three things worth knowing:

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

- **`proposeJobOpening()`** — the AI proposal step. It becomes a `CriteriaDeriver` port beside
  the AI Service client that UC-3 already uses (ADR-007).
- **The UC-3 precondition.** The use case says a guide is for a *shortlisted* candidate;
  shortlisting is not built, so nothing checks it.
- **`recordPipelineEvent()`** — reporting openings to the dashboard. The docs say this push needs
  an ADR before it is built.
- **Role enforcement** (FR-0.4).
- **A readiness endpoint** that checks the database (ADR-005 rule 4 asks for one; only `/healthz`
  exists).
- **A migration tool.** The schema is applied idempotently at startup until the first change that
  cannot be expressed that way.
- Clients generated from the spec — there is no consumer yet.
