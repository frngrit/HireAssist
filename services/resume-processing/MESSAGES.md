# Screening messages: Hiring ⇄ Resume Processing (RabbitMQ)

The contract for UC-2 between the Hiring Service (A) and the Resume Processing
Service (B). Both sides must agree on this file; change it in one PR that both
owners approve.

## Queues

| Queue | Direction | Declared by | Arguments |
|---|---|---|---|
| `screening.requested` | Hiring → Resume Processing | either side | durable, none |
| `screening.completed` | Resume Processing → Hiring | either side | durable, none |
| `screening.requested.retry` | internal to Resume Processing | Resume Processing | dead-letters back to `screening.requested` |
| `screening.requested.dlq` | internal to Resume Processing | Resume Processing | durable, none |

- Messages go through the default exchange, routed by queue name.
- The two shared queues have **no arguments**, so either side may declare them
  (`assertQueue(name, { durable: true })`) without a `PRECONDITION_FAILED` clash.
- Publish with `persistent: true` and `contentType: application/json`.
- Put the correlation id in both the body and the `x-correlation-id` header (ADR-005).

## `screening.requested`: one message per resume

Hiring publishes this after storing the file in MinIO.

```json
{
  "batch_id": "batch-001",
  "resume_id": "res-001",
  "job_opening_id": "job-001",
  "file": { "bucket": "resumes", "key": "screening/batch-001/res-001.pdf" },
  "criteria": [
    { "id": "c1", "text": "3+ years building backend services", "must_have": true, "weight": 0, "category": "experience" },
    { "id": "c2", "text": "Strong SQL", "must_have": false, "weight": 40, "category": "skills" }
  ],
  "language": "en",
  "correlation_id": "6f1c…"
}
```

| Field | Required | Meaning |
|---|---|---|
| `batch_id` | yes | The screening batch (owned by Hiring) |
| `resume_id` | yes | The resume inside the batch. **`(batch_id, resume_id)` is the idempotency key**: sending the same pair twice is safe |
| `job_opening_id` | no | Copied onto the result for convenience |
| `file.bucket`, `file.key` | yes | Where the PDF is in MinIO |
| `criteria` | yes, non-empty | **Snapshot** of the opening's criteria at submission time, same shape as `Criterion` in `proto/hireassist/ai/v1/ai_service.proto` |
| `language` | no | `en` or `th` |
| `correlation_id` | no | Generated if missing |

There is **no `workspace_id`**: one deployment serves one company (ADR-006).
An extra field is ignored, so an older sender that still includes it keeps working.

## `screening.completed`: one message per resume, whatever the outcome

```json
{
  "batch_id": "batch-001",
  "resume_id": "res-001",
  "job_opening_id": "job-001",
  "candidate_id": "cand-001",
  "status": "scored",
  "score": 78,
  "meets_must_haves": true,
  "unmet_must_have_ids": [],
  "justification": "Five years of Java and PostgreSQL at …",
  "error": null,
  "correlation_id": "6f1c…"
}
```

| `status` | Meaning | `score` / `meets_must_haves` | `error` |
|---|---|---|---|
| `scored` | Parsed, profiled and scored | set | null |
| `unparsable` | The PDF has no readable text (for example a scanned image). Flag it for manual review (FR-2.7) | null | why |
| `failed` | Gave up: the file is missing, the message is invalid, or the AI Service stayed down through every retry | null | why |

For the per-criterion evidence behind a score, Hiring calls
`GET /candidates/{candidate_id}` on Resume Processing (REST, see `openapi.yaml`).

## Retries and dead-lettering (inside Resume Processing)

- **Transient failure** (AI Service or MinIO unreachable): the message goes to
  `screening.requested.retry`, waits there, and comes back. The wait starts at
  `RETRY_DELAY_MS` (default 5 s) and doubles each time, ±20% jitter: about 5,
  10, 20 s. After `MAX_RETRIES` (default 3) it is dead-lettered.
- **Permanent failure** (invalid message, missing file, AI rejects the input):
  dead-lettered at once.
- Either way, a `failed` result is published so Hiring never waits forever.
- Dead-lettered messages stay in `screening.requested.dlq` for a person to
  inspect. `npm run replay-dlq` moves them back once the cause is fixed.
