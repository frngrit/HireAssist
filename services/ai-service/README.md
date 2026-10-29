# AI Service

The only component in HireAssist that calls a language model. Every other service asks it for a
domain answer — criteria, a score, interview questions — and never for a completion, so the
provider, the model name, the prompt and the token budget appear in no other service's contract
(ADR-004).

Reached over **gRPC** and never exposed through the API gateway (ADR-007). It owns no domain data:
prompts are configuration and the model credential is a secret.

## Operations

| RPC | Use case | In → out |
|---|---|---|
| `DeriveCriteriaFromDescription` | UC-1 | free-text description → proposed title, weighted criteria |
| `ExtractProfileFromText` | UC-2 | resume text → structured candidate profile |
| `ScoreAgainstCriteria` | UC-2 | profile, criteria → score, must-have check, justification |
| `GenerateInterviewQuestions` | UC-3 | profile, criteria, emphasis → tagged questions |

`ExtractProfileFromText` runs once per resume and its result is cached by the caller;
`ScoreAgainstCriteria` runs once per resume **per job opening**, which makes it the
highest-volume call in the system.

`ExtractProfileFromText` is the only operation that receives unstructured resume text, and so the
only one that sees the attributes excluded from scoring — dropping them is part of its job
(ADR-008). Extracting text *from the PDF* stays deterministic and stays in Resume Processing; no
model receives a file. The contract lives in [`proto/hireassist/ai/v1/ai_service.proto`](../../proto/hireassist/ai/v1/ai_service.proto)
at the repository root, because callers generate clients from the same file.

## Running it

Requires [uv](https://docs.astral.sh/uv/) and Python 3.12.

```sh
make install    # create the venv, install dependencies
make proto      # generate gRPC stubs from the shared .proto
make run        # start the server on :50051
make test
make lint
```

Generated stubs land in `src/ai_service/gen/` and are **not committed** — regenerate them after any
change to the `.proto`.

Reflection is on, so tooling can discover the contract without the file:

```sh
grpcurl -plaintext localhost:50051 list
grpcurl -plaintext localhost:50051 grpc.health.v1.Health/Check
```

## Demonstrating it

```sh
make demo-offline     # starts a server with canned answers, runs the walkthrough, stops it
```

```sh
make run              # terminal 1, with AI_SERVICE_API_KEY set
make demo             # terminal 2 — the same walkthrough against a live model
```

`demo/demo_client.py` walks the three operations in order and prints what crosses the wire:
the service describing itself over reflection, the health check, UC-1 → UC-2 → UC-3, and a call
the contract refuses. It takes an optional `host:port`.

`AI_SERVICE_PROVIDER=canned` answers from a script with no credential and no network, so the
demo can be rehearsed offline and survives a dead connection on the day. Every canned answer is
labelled in its own text and in the attribution the caller receives (`provider=canned`,
`model=canned-answers-no-model-was-called`), so it cannot be mistaken for a real one on a
projector.

## Serving

**Health** follows the standard gRPC health protocol on `grpc.health.v1.Health`, under both
`hireassist.ai.v1.AiService` and the empty service name. It reports whether the instance can do its
job, not whether the port answers: with no credential it is reachable but useless, so it reports
`NOT_SERVING` and an orchestrator keeps traffic away rather than sending every resume to a
`FAILED_PRECONDITION`.

**Shutdown** drains. On SIGTERM or SIGINT the server stops accepting calls and gives the ones
already running `AI_SERVICE_SHUTDOWN_GRACE_S` to finish. A scoring call in flight has already been
paid for and its resume is claimed by a work row; killing it mid-answer wastes both and sends the
row round the retry loop for nothing.

**Accounting.** Every answered call logs one line with the model, the elapsed time and the input
and output token counts. This is the only place usage is observable — ADR-004 puts a token budget
on a deployment, and a budget nothing counts against is a sentence in a document.

## Configuration

Environment variables, all prefixed `AI_SERVICE_`:

| Variable | Default | Meaning |
|---|---|---|
| `AI_SERVICE_PORT` | `50051` | gRPC listen port |
| `AI_SERVICE_MAX_WORKERS` | `10` | server thread pool size |
| `AI_SERVICE_PROVIDER` | `anthropic` | `anthropic` or `openai` — the two ADR-004 names — or `canned` for scripted demo answers; empty means the default |
| `AI_SERVICE_MODEL` | the provider's: `claude-haiku-4-5-20251001` or `gpt-4.1-mini` | model to call; attribution records what actually answered |
| `AI_SERVICE_MAX_TOKENS` | `4096` | ceiling on one answer |
| `AI_SERVICE_PROMPT_VERSION` | *(built-in)* | overrides the version stamped on answers; leave unset |
| `AI_SERVICE_API_KEY` | *(unset)* | the chosen provider's credential; the service refuses to score without it |
| `AI_SERVICE_REQUEST_TIMEOUT_S` | `60` | per-attempt bound, after which the caller retries |
| `AI_SERVICE_SHUTDOWN_GRACE_S` | `30` | how long an in-flight call has to finish after SIGTERM |
| `AI_SERVICE_ENABLE_REFLECTION` | `true` | serve gRPC reflection, so tooling needs no `.proto` |

Copy `.env.example` to `.env` and put the key there. `.env` is gitignored.

## How a call is answered

Every handler does the same four things: render the request into a prompt, ask the provider for an
answer conforming to a JSON schema, validate it, and map it onto the proto response. Structured
output comes from forcing a tool call, not from asking the model for JSON, so the schema is
enforced by the API rather than defended against in every handler.

Two properties are worth knowing before reading the code.

**The overall score is arithmetic, not a model output.** The model judges each criterion; the
weighted total is computed in `answers.weighted_total`. A weighted average has a right answer, and
a model that returns a total inconsistent with its own per-criterion judgements leaves a Recruiter
two numbers to reconcile. Must-have criteria carry no weight — they gate the candidate through
`meets_must_haves` — so an opening of nothing but must-haves falls back to the unweighted mean.

**Nothing degrades.** An answer that does not match the criteria supplied, a score with no
justification, or an extraction that came back empty in every field is an error rather than a
partial result: an empty or wrong answer ranks a candidate last, which is the failure the product
exists to prevent. A sparse profile is not the same thing — a new graduate with education and no
work history is an ordinary resume, and is accepted.

| Status | Means | Caller should |
|---|---|---|
| `FAILED_PRECONDITION` | no model credential configured | fix the deployment; retrying is futile |
| `INVALID_ARGUMENT` | unusable request — scoring with no criteria, an empty description | fix the call |
| `UNAVAILABLE` | the provider could not be reached or refused | retry with backoff (ADR-002) |
| `INTERNAL` | the model answered, but not usably | worth one retry, then `needs_manual_review` |

## Layout

| File | Holds |
|---|---|
| `service.py` | the three handlers, and the proto ↔ domain mapping |
| `prompts.py` | the prompts, their JSON schemas, and `PROMPT_VERSION` |
| `answers.py` | what the model must return, validated, plus the score arithmetic |
| `provider.py` | the vendor boundary: one protocol, one error type |
| `providers/anthropic_provider.py`, `providers/openai_provider.py` | the only files that import a vendor SDK, one per provider ADR-004 names |
| `providers/fake.py` | a scripted provider, so the suite never makes a live call |

Changing a prompt changes `PROMPT_VERSION`. It travels with every answer in `ModelAttribution`, so
an edited prompt under an unchanged version makes past attributions lie.

## Status

All three RPCs implemented, with health, reflection, draining shutdown and usage accounting. The
suite is structurally incapable of reaching a vendor — constructing a real provider inside a test
raises — which is also the limit of what it proves: **no call has yet been made against a live
model**, so the prompts are unvalidated. Set `AI_SERVICE_API_KEY` and score one real resume to
close that.

Not done here, because each is a decision rather than an omission: the container image and
deployment pipeline (open, and named as such in ADR-006), and registration with service discovery.
