# Demo resumes

Three **synthetic** resumes for demonstrating UC-1 → UC-2 → UC-3. Every person, company, school
and contact detail is invented. Never add a real resume here (PDPA).

| File | Meant to show |
|---|---|
| `1-strong-match-ananda.pdf` | Meets both must-haves with room to spare and every nice-to-have, including strong DevOps (CI/CD, Terraform, monitoring, on-call): ranked first, shortlisted, then given an interview guide |
| `2-partial-match-kanya.pdf` | Meets both must-haves, just (three years of Go), and some nice-to-haves (Docker, a little RabbitMQ, a basic CI pipeline; no Kubernetes, infrastructure as code, monitoring or payments): qualified, ranked lower |
| `3-no-match-pimchanok.pdf` | A designer, with neither must-have: marked not qualified, and cannot be shortlisted |

The score is the weighted average of the **nice-to-haves** only; must-haves gate a candidate
rather than add to the score. A resume that meets every must-have and no nice-to-have scores 0,
which is why the partial match has some of them.

Scores come from the AI Service, so they need a real model (`AI_SERVICE_PROVIDER` and
`AI_SERVICE_API_KEY` in the root `.env`). With canned answers every resume scores the same.

## The job opening to screen them against

Paste this into **New job opening** to have the criteria proposed (UC-1):

> Backend engineer for our payments team. Must have 3+ years building backend services in Go
> and production experience with PostgreSQL. Experience with Kafka or RabbitMQ is a plus, as is
> Docker and Kubernetes. A payments or fintech background is preferred. DevOps experience matters
> most to us: CI/CD pipelines, infrastructure as code and monitoring.

Check that the proposal is close to these criteria before creating the opening; edit it if not:

| Criterion | Classification | Weight |
|---|---|---|
| 3+ years building backend services in Go | must-have | 5 |
| Production experience with PostgreSQL | must-have | 5 |
| Kafka or RabbitMQ | nice-to-have | 3 |
| Docker and Kubernetes | nice-to-have | 2 |
| Payments or fintech background | nice-to-have | 2 |
| DevOps experience (CI/CD, infrastructure as code, monitoring) | nice-to-have | 5 — the highest |

## Changing them

Edit `make_resumes.py` and rebuild:

```sh
uv run --with reportlab demo/resumes/make_resumes.py
```
