# HireAssist

An AI-assisted hiring-support layer for companies that receive more applications than they
can screen carefully and interview well.

HireAssist is **not** a job board, **not** a job-application platform, and **not** another
applicant-tracking system. It attaches to the hiring channels a company already uses and
supplies the judgement capacity it lacks: screening every resume with a stated reason,
preparing interview questions grounded in the specific resume and role, surfacing positions
that have stalled, and keeping candidate data compliant with Thailand's PDPA.

> The human still decides. HireAssist makes sure they are deciding on evidence they actually
> had time to look at.

Term project for **Software Architecture**, Chulalongkorn University.

---

## Team

| Student ID | Name | Nickname |
|---|---|---|
| 6772055221 | Patiphon Puntusin | Me |
| 6872036421 | Thanabul Parodom | Tim |
| 6870406621 | Thanwarat Korcharoenkiat | Yo |
| 6870235621 | Rerngrit Jangsri | Frank |

---

## Documentation

| Document | What it is |
|---|---|
| [docs/PROPOSAL.md](docs/PROPOSAL.md) | **The submission document.** Project description, target customers, use cases, requirements, ADRs. Source of truth. |
| [docs/FUNCTIONAL-REQUIREMENTS.md](docs/FUNCTIONAL-REQUIREMENTS.md) | 50 functional requirements, traced to the use case each serves. |
| [docs/NON-FUNCTIONAL-REQUIREMENTS.md](docs/NON-FUNCTIONAL-REQUIREMENTS.md) | 17 non-functional requirements, grouped by quality attribute. |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Architecture diagram (version 1): the components, what each owns, and how to read the picture. |
| [docs/diagrams/](docs/diagrams/) | Diagrams as code — the use case diagram (PlantUML source + rendered SVG) and the architecture diagram (hand-laid SVG). |
| [docs/SERVICE-OPERATIONS-COLLABORATORS.md](docs/SERVICE-OPERATIONS-COLLABORATORS.md) | Each service's operations and who it collaborates with. |
| [services/ai-service/](services/ai-service/) | **Code.** The AI Service — Python, uv, gRPC. The first implemented service. |
| [services/identity/](services/identity/) | **Code.** The Identity Service — Go, net/http, PostgreSQL, REST. Member CRUD and signing in (UC-0): ES256 access tokens and rotating refresh tokens (ADR-009). |
| [services/hiring/](services/hiring/) | **Code.** The Hiring Service — Go, PostgreSQL and MongoDB, REST with an OpenAPI contract. Job openings (UC-1) and interview guides (UC-3); proposing criteria with AI is not built yet. |
| [services/gateway/](services/gateway/) | **Code.** The API Gateway — Go. Routing, local token verification (ADR-009), the role gate and rate limiting. |
| [proto/](proto/) | Shared gRPC contracts. Callers generate clients from these. |
| [services/web/](services/web/) | **Code.** The HireAssist Web UI — React, TypeScript, Mantine. Sign-in and UC-1 to UC-3. Runs on a mock backend, or against the gateway for sign-in, job openings and interview guides. |
| [docs/OVERVIEW.md](docs/OVERVIEW.md) | Runtime view — what each service owns, who calls whom, and the call sequence behind each use case. |
| `docs/build-proposal-docx.py`, `docs/adr/build-docx.py` | Build the two Word deliverables. The documents themselves are generated, not committed. |
| [docs/CONTEXT.md](docs/CONTEXT.md) | Internal working notes — open questions, open decisions, glossary. Not for submission. |
| [docs/adr/](docs/adr/) | Architecture Decision Records, one decision per file, with an index. |
| [docs/course/ASSIGNMENT.md](docs/course/ASSIGNMENT.md) | The assignment brief and submission guideline this proposal is written against. |
| [docs/course/](docs/course/) | Course requirements and grading breakdown. |
| [docs/reference/](docs/reference/) | The lecturer's ADR samples (one good, one deliberately poor). |
| [CHANGELOG.md](CHANGELOG.md) | Dated summary of what changed in the docs, and why. |

---

## Use cases

| ID | Use Case | Primary Actor |
|---|---|---|
| UC-0 | Sign in | Guest |
| UC-1 | Create a job opening from natural-language requirements | Recruiter |
| UC-2 | Batch-screen resumes against a job opening | Recruiter |
| UC-3 | Generate candidate-specific interview questions | Recruiter |
| UC-4 | Monitor hiring pipeline and stale positions | Recruiter |
| UC-5 | Enforce candidate data retention | System Scheduler / Admin |
| UC-6 | Manage members and roles | Admin |

One use case — talent-pool re-matching — is deliberately deferred and recorded as **D-1** in
the proposal.

---

## Architecture decisions

Architecture Decision Records live in **[`docs/adr/`](docs/adr/)**, one decision per file, with the
index and the decisions not yet taken in [`docs/adr/INDEX.md`](docs/adr/INDEX.md).

---

## Repository status

**Documentation, plus four services in progress:** the AI Service, the Identity Service, the
Hiring Service and the API Gateway, each listed in the table above with its own README.
`docker compose up -d --build` at the root runs the whole system: the web app on
http://localhost:4173, the gateway, the Identity, Hiring, Resume Processing and AI services
(the AI Service with canned answers, so no API key is needed), PostgreSQL, MongoDB, RabbitMQ
and S3 storage. The comments at the top of `docker-compose.yml` list the demo sign-ins and the
variables that move a host port that is already taken.

The repository is a monorepo. Services live at `services/<name>/` and shared gRPC contracts at
`proto/`. That layout was settled by starting implementation and still needs an ADR. Each
service's language is its owner's choice under ADR-005, which is still *Proposed*. The gateway
is owned by the whole team jointly (ADR-005 rule 5), and it is written rather than configured from an off-the-shelf
gateway because ADR-009 asks it to verify tokens itself and to record a rejected request — Go for
consistency with the two services it fronts.

```
hire-assist/
├── README.md
├── .gitignore
├── docker-compose.yml       # local stack: every service, datastore and the broker
├── deploy/                  # container support files (database init, development signing keys)
├── proto/                   # shared gRPC contracts
├── services/
│   ├── ai-service/          # AI Service (Python, gRPC)
│   ├── identity/            # Identity Service (Go) — members
│   ├── hiring/              # Hiring Service (Go) — job openings
│   └── gateway/             # API Gateway (Go) — routing, tokens, roles
└── docs/
    ├── PROPOSAL.md          # submission document
    ├── CONTEXT.md           # working notes
    ├── diagrams/            # diagrams as code (.puml) + rendered .svg
    ├── adr/                 # architecture decision records
    ├── course/              # course requirements & grading
    └── reference/           # lecturer-provided samples
```

---

## Working agreements

- `docs/PROPOSAL.md` is the single source of truth. If something is settled, it lives there
  and is not duplicated elsewhere.
- Every significant architectural decision gets an ADR in `docs/adr/`, using
  [TEMPLATE.md](docs/adr/TEMPLATE.md), and is listed in [INDEX.md](docs/adr/INDEX.md).
- Every documentation change gets a `CHANGELOG.md` entry saying **why**, not just what.
- Commit under your own account — per-member Git contribution is part of the project grade.
