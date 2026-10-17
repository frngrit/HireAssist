# Resume Processing Service

Owns candidate profiles and screening results for HireAssist, and runs the
UC-2 screening pipeline: one resume in, one scored (or flagged) result out.

```
Hiring ──RabbitMQ: screening.requested──►  Resume Processing
                                             1. fetch the PDF from MinIO (S3 API)
                                             2. PDF → text (local, deterministic)
                                             3. AI Service.ExtractProfileFromText  (gRPC)
                                             4. AI Service.ScoreAgainstCriteria    (gRPC)
                                             5. save profile + result in MongoDB
Hiring ◄──RabbitMQ: screening.completed────┘
Hiring ──REST: GET /candidates/{id}──────►  profile + per-criterion evidence
```

| Decision | Where it comes from |
|---|---|
| RabbitMQ between Hiring and this service, with retry and dead-letter | ADR-010 (replaces the REST handover and work table of ADR-002; keeps its one-resume-one-unit rule) |
| gRPC to the AI Service, from the shared `proto/` | ADR-007, ADR-005 |
| REST for everything else | ADR-007 |
| MongoDB for profiles and scoring output, keyed by `candidate_id` only | ADR-003, ADR-006 |
| Resume text is extracted here, never sent as a file to the AI Service | `ai_service.proto` (`CandidateProfile`) |

The message contract with Hiring is in **[MESSAGES.md](MESSAGES.md)**; the REST
contract is in **[openapi.yaml](openapi.yaml)**.

## Owners

| Role | Name |
|---|---|
| Primary | Thanabul Parodom |
| Secondary | _to be named by the team_ |

## Files

| Path | Purpose |
|---|---|
| `server.js` | Entry point: REST server + RabbitMQ consumer |
| `src/queue.js` | Consume `screening.requested`, publish `screening.completed`, retry and dead-letter |
| `src/pipeline.js` | The five steps above, idempotent per `(batch_id, resume_id)` |
| `src/storage.js` | MinIO / S3 adapter |
| `src/pdf.js` | PDF → text; flags scanned PDFs as `unparsable` |
| `src/ai.js` | gRPC client generated from `proto/hireassist/ai/v1/ai_service.proto` |
| `src/db.js` | MongoDB models |
| `src/rest.js` | REST endpoints |
| `scripts/fake-hiring.js` | Plays the Hiring Service: uploads resumes, publishes requests, prints results |
| `scripts/replay-dlq.js` | Moves dead-lettered messages back for another try |
| `client.js` | The manual CRUD walkthrough from the REST milestone |
| `samples/` | Three made-up resumes: two readable, one "scanned" with no text |
| `docker-compose.yml` | RabbitMQ, S3 object storage (moto), MongoDB and the AI Service (canned answers) for local runs |
| `Dockerfile`, `k8s/` | Container image (build from the repo root) and Kubernetes manifest |

## Run it locally

Needs Docker Desktop and Node.js 18+, and the **whole repository** (the service
reads the AI contract from `proto/` and the compose file starts
`services/ai-service`).

```bash
cd services/resume-processing
docker compose up -d        # RabbitMQ, S3 storage, MongoDB, AI Service (first start takes a minute)
npm install
npm start                   # terminal 1: the service
npm run fake-hiring         # terminal 2: send the sample resumes through the pipeline
```

`fake-hiring` prints each `screening.completed` as it arrives and then the
ranked list. Two readable resumes come back `scored`; the scanned one comes back
`unparsable` for manual review.

Things worth looking at while it runs:

| What | Where |
|---|---|
| Queues and message rates | http://localhost:15672 (guest / guest) |
| Uploaded resumes | `curl http://localhost:9000/resumes` (S3 bucket listing, XML) |
| Results of a batch | `curl "http://localhost:3000/screenings?batchId=<batch id>"` |
| One candidate with evidence | `curl http://localhost:3000/candidates/cand-001` |
| Readiness | `curl http://localhost:3000/readyz` |

### Showing retry and dead-letter

```bash
npm run fake-hiring -- --missing   # one request points at a file that does not exist → failed, dead-lettered at once
docker compose stop ai-service     # AI down: requests retry 3 times (about 5, 10, 20 s), then dead-letter
npm run fake-hiring
docker compose start ai-service
npm run replay-dlq                 # move them back; they are scored this time
```

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `PORT` | `3000` | REST port |
| `MONGO_URL`, `MONGO_DB` | `mongodb://127.0.0.1:27017`, `hireassist_resume_processing` | MongoDB (Atlas works too) |
| `AMQP_URL` | `amqp://guest:guest@127.0.0.1:5672` | RabbitMQ |
| `S3_ENDPOINT`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_REGION` | `http://127.0.0.1:9000`, `minioadmin`, `minioadmin`, `us-east-1` | MinIO |
| `AI_SERVICE_ADDR` | `127.0.0.1:50051` | AI Service gRPC address |
| `AI_PROTO_PATH` | `../../proto/hireassist/ai/v1/ai_service.proto` | The shared contract |
| `MAX_RETRIES`, `RETRY_DELAY_MS`, `PREFETCH` | `3`, `5000`, `4` | Retries, first retry delay (doubles each time, ±20%), and how many resumes one replica runs at once |

Logs are one JSON object per line; every request and every screening carries a
`correlation_id` (ADR-005).

## Known gaps

- **Caller checks on REST reads.** The endpoints do not yet check who is
  calling. That waits for the Gateway and Identity to settle how identity
  reaches internal services. (There is no workspace check to do: one deployment
  serves one company, ADR-006.)
- **Candidate identity in MongoDB.** ADR-003 puts identity and retention dates
  in PostgreSQL; this service keeps everything in MongoDB for now.
- **Canned AI answers locally.** The compose file runs the AI Service with
  `AI_SERVICE_PROVIDER=canned`, so every resume gets the same scripted profile
  and score. Set `AI_SERVICE_API_KEY` and remove the provider override for
  real scoring.
- **OpenTelemetry tracing.** The correlation id is propagated and logged, but
  no traces are exported yet.
