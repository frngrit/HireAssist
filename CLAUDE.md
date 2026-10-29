# CLAUDE.md — working rules for this repository

HireAssist is a Software Architecture term project. It is a **monorepo**: the documentation in
`docs/`, and from 2026-09-24 the services themselves under `services/` with shared gRPC contracts
in `proto/`.

The documents *are* the graded deliverable, so keeping them accurate is not housekeeping — it is
the work. Code is held to the ordinary standard of code.

---

## The core rule

**Every change or decision updates the docs in the same session it is made.**

Not "later", not "before submission". A decision that lives only in a chat transcript is lost.
If we agree something in conversation, it is not done until it is written down in the right
file, every place that contradicts it has been fixed, and `CHANGELOG.md` records it.

---

## The changelog is not optional

**Any change to the documentation gets an entry in `CHANGELOG.md`. Changes to code do not.**

`CHANGELOG.md` is the log of the *documentation* — what was decided about the system and why. Git
history is the log of the code. Writing a new service, adding a dependency, fixing a bug or
scaffolding a package is a commit message, not a changelog entry, and putting it here buries the
decisions the changelog exists to hold.

The line is about *decisions, not files*. When work on the code settles something that was open in
the documents — a layout that was an open question, a language that was undecided, a contract
shape an ADR left abstract — that decision is documentation and gets an entry, describing the
decision rather than the code that revealed it. The commit still carries the code.

It is a *summary* log, not a diff — Git already has the diffs. Record the decision and the
reason for it, in prose a teammate can read months later. The test: if someone could
reasonably ask *"why did we do that?"*, the answer belongs in the changelog.

- **One entry per working session**, newest first, headed `YYYY-MM-DD HH:MM`. Start a new entry
  at the top — never edit one that is already committed. An entry records what a session decided;
  rewriting it destroys when and why things changed.
- **Keep it short.** A line or two per change: what changed, and why in a clause. Aim for an entry
  you can read in under a minute. If an explanation needs a paragraph, it belongs in the ADR or
  the document, and the entry points there instead of repeating it — the reasoning has a home, and
  duplicating it into the changelog means two versions that drift.
- Write **why**, not just what. "Deferred re-matching" is useless; "deferred re-matching — cannot
  be demonstrated without a deep talent pool" is the point. One clause is usually enough.
- Record decisions that were *reversed* or *reconsidered* too, and name the entry a later one
  overturns rather than amending the earlier entry.
- End with what is still outstanding, in a line.

A `PostToolUse` hook (`.claude/settings.json` → `.claude/hooks/changelog-reminder.sh`) injects
this reminder automatically whenever any file under `docs/` is actually modified — and only
`docs/`, which is the right scope for the rule above: editing a service does not ask for an entry. It compares
modification times against a marker rather than reading the tool's arguments, so it catches an
edit however it was made, stays silent when a file was only read, and reports a change once. It
is POSIX `sh` with no interpreter dependency, so it runs on any machine with a shell. It is a
safety net, not the rule — the obligation is this section, and the hook only fires inside a
Claude Code session that has it loaded.

---

## Where things live

| File | Holds | Never holds |
|---|---|---|
| `docs/PROPOSAL.md` | **Source of truth.** Everything settled: name, problem, customers, use cases, requirements, ADR summaries. | Unresolved questions, internal deliberation |
| `docs/CONTEXT.md` | Only what is *not* settled: open questions, open decisions, glossary, provenance. | Anything already stated in PROPOSAL.md |
| `docs/adr/` | One file per architectural decision, plus `INDEX.md`. | Decisions still being debated |
| `docs/OVERVIEW.md` | The **runtime view**: per-service ownership and the call sequence behind each use case. Defers to PROPOSAL (use cases), the SOC table (operations) and the ADRs (why) — correct it whenever any of those change. | Anything it would be the source of truth for |
| `README.md` | Orientation: pitch, team, doc index, use case list, repo status. | Detail that belongs in PROPOSAL.md |
| `CHANGELOG.md` | A dated summary of what changed **in the documentation** and why, newest first. | File-by-file diffs; code changes, which belong in commit messages |

**No duplication between `PROPOSAL.md` and `CONTEXT.md`.** They overlapped once and it went
stale immediately. When something moves from open to settled, it moves *out* of CONTEXT and
*into* PROPOSAL — copying is not moving.

---

## When a use case changes

Use cases are referenced from many places. Changing one means touching all of these — check
every one, every time:

1. The use case's own section in `docs/PROPOSAL.md`
2. The **overview table** at the top of *Scenario (use-case & description)*
3. The **actors table** — actor descriptions name the use cases they participate in
4. The **use case diagram** — `docs/diagrams/use-case-diagram.puml` (PlantUML): use cases,
   associations, relationships. Re-render `use-case-diagram.png` from it and commit both together
   (`plantuml -tpng docs/diagrams/use-case-diagram.puml`); the proposal embeds the PNG, so a
   stale image is a stale diagram
5. The **relationships rationale** below the diagram, if any «include»/«extend» is affected
6. **Cross-references inside other use cases** — grep for `UC-` and read each hit in context
7. The use case list in `README.md`
8. The provenance table in `docs/CONTEXT.md`
9. `CHANGELOG.md` — what changed and why

**Requirement IDs** (`FR-<uc>.<n>`) are stable once written and never reused. A withdrawn
requirement is **removed from `docs/FUNCTIONAL-REQUIREMENTS.md` entirely** — not struck through —
leaving a gap in the sequence. Every block was compacted once, on 2026-09-12, by explicit decision
while nothing had been submitted; that was a one-off and is not a precedent. Renumbering rewrites
requirement references inside ADRs, which are point-in-time records — once anything is submitted,
gaps stay.

**A withdrawn requirement must never be referred to by its id after a renumbering.** Its old
number now belongs to a different requirement, so the reference resolves silently to the wrong
thing — worse than dangling, because nothing detects it. Name the withdrawn requirement in words
instead. Three ADRs referred to FR-0.3 (workspace scoping) this way and were rewritten.

`docs/FUNCTIONAL-REQUIREMENTS.md` states what the system does today; why something was withdrawn
belongs in `CHANGELOG.md`, and in the ADR that caused it.

Stale cross-references have been the single most common defect in this repo. After any
renumbering, grep for every `UC-` and `D-` reference and verify each one still points at what
it claims.

**Use case IDs:** UC-0 through UC-6 active, D-1 deferred. A new use case takes the next free
number rather than being slotted in by topic — renumbering would renumber every `FR-<uc>.<n>`
behind it, and requirement IDs are stable once written. If a use case is removed, close the
numbering gap and re-check every reference. If one is deferred rather than dropped, it moves
to the *Deferred Use Cases* section with a stated reason — deferral is a decision and gets
recorded like one.

---

## ADRs

Use `docs/adr/TEMPLATE.md`. It follows the **Jeff Tyree & Art Akerman** template from the
course slides (see *Context outside this repository* below), grouped as *Summary · Details · Related · Notes*, with
our additions: a Date/Deciders header, a one-sentence Alexandrian summary, and explicitly
optional fields. The full guidance — including what the lecturer's deliberately bad sample
gets wrong — lives in that file.

- **Positions is the field that matters.** If you cannot name a rejected alternative, it is
  not an ADR.
- **Implications must include the negatives.** An all-positive ADR has not examined the
  trade-off.
- **Related requirements is not optional.** Map every decision to the use cases it serves and
  to `docs/course/REQUIREMENTS.md` — traceability is graded.
- **An ADR must stand alone.** It will be read outside this repository, so it may not reference
  project documents — no "see the proposal", no file paths. State the fact instead. Requirement
  and use case ids are fine, but say what each means the first time it appears.
- **Keep it to one or two pages.** A long ADR is not more rigorous, only less likely to be read.
- Skip optional fields rather than padding them with "N/A". Filler hides the real content.
- Write the ADR when the decision is made. Reconstructing the reasoning later does not work —
  the rejected alternatives are exactly what gets forgotten.
- Add the `INDEX.md` row in the same commit as the ADR.
- **Never edit an existing ADR because of a new one** — not the body, not the Status. The records
  are a stack: append, never reach back. An earlier ADR describing something no longer true is
  correct; it records a decision made on a date. `docs/adr/INDEX.md` is the living view and carries
  what each record has since changed. Revise an ADR only when explicitly asked to.
- **To change a decision, write a new ADR** that names the earlier one and states what it
  overrides. That new record is the only file written — there is no *Superseded* status to set and
  nothing to mark on the old one. Push, never reach back.
- A decision recorded in an ADR that contradicts `PROPOSAL.md` means `PROPOSAL.md` is now
  wrong — fix it.

---

## Context outside this repository

Some material that shaped this project is **not in the repo** and is not available to a fresh
clone or a new session. Do not link to it by path and do not assume anyone can open it.

| What | Where it actually is | How we handle it |
|---|---|---|
| Course slide decks (`4-1-ADRs.pdf`, decomposition, DDD, …) | The team's own machines, under the course folder — *outside this repo* | Anything we rely on gets summarised into a repo document. The ADR template already carries what we needed from the ADR deck. |
| Course syllabus | Same — outside this repo | Deliberately not committed |
| The lecturer's ADR samples | **In the repo**, `docs/reference/` | Safe to reference |
| Course requirements & grading | **In the repo**, `docs/course/` — copies | Safe to reference |
| The assignment brief and submission guideline | **In the repo**, `docs/course/ASSIGNMENT.md` | Safe to reference |
| The architecture diagram | **Version 2 is in the repo as code**: `docs/diagrams/sw-arch-v2.svg`, rendered to `sw-arch-v2.png` with `rsvg-convert -z 2` — edit the SVG and re-render, committing both. Version 1, `docs/diagrams/sw-arch.drawio.png`, was drawn in draw.io with its source outside the repo and is kept for history. | Check the diagram against the Service–Operations–Collaborators table whenever either changes. |
| The ADR Word export, `HireAssist-ADRs.docx` | Moved **outside this repo**, to the sibling `word/` folder | Not linked from the repo. Regenerate with `docs/adr/build-docx.py` if it is needed again — the markdown in `docs/adr/` is the source. |
| Verbal guidance given in class, or decisions made in chat | Nowhere, unless written down | This is the dangerous one — see below |

**The rule:** if a decision, constraint or piece of guidance influences the documents, it must
be captured *inside the repo* — in `docs/course/ASSIGNMENT.md`, `docs/PROPOSAL.md`,
`docs/CONTEXT.md`, an ADR, or `CHANGELOG.md`. A reference to something only one person can
open is not documentation.

When summarising external material into the repo, say where it came from ("the course slides
present Tyree & Akerman…") so a reader knows the provenance without needing the source. Do not
commit the source files themselves — they are the lecturer's material, and the repo stays
documentation we wrote.

---

## Terminology

Use the glossary in `docs/CONTEXT.md`. The terms are deliberate and consistent across
documents: *Job Opening, Criterion, Candidate Profile, Screening Batch, Screening
Score, Justification, Decision, Interview Guide, Talent Pool, Stale Position, Retention Policy.*

If a new domain term is needed, add it to the glossary rather than improvising a synonym.
Silent synonyms are how a domain model rots.

---

## Writing style for these documents

- Explain *why*, not just *what*. A grader is reading for understanding of the system's
  behaviour and scope — an ambiguous or over-terse use case loses marks explicitly.
- State trade-offs honestly, including negative consequences. Sections that are entirely
  positive read as unexamined.
- Scope decisions are load-bearing: when something is deliberately excluded, say so and say
  why. "Not a job board", "not an ATS", D-1 deferred — these are decisions, not gaps.
- Prefer prose that a reader can follow over bullet fragments that need decoding. Tables are
  for genuinely tabular material.

---

## Constraints that shape every decision

- **Course requirements** (`docs/course/REQUIREMENTS.md`): REST + gRPC + message broker, API
  gateway, service discovery, RDBMS + NoSQL, 2 load tests, risk matrix, one demonstrated quality
  attribute. All three styles are now in place: REST on every boundary (ADR-001), gRPC for the AI
  Service (ADR-007), and RabbitMQ for the screening handover between Hiring and Resume Processing
  (ADR-013). Moving anything else onto the broker — the pipeline events are the obvious next
  candidate — is a new ADR, not a quiet change to a document.
- **PDPA** is a first-class design force, not a feature. Consent and retention gate what the
  system may do with candidate data.
- **Never commit real resumes or candidate data.** `/data/` and `/uploads/` are gitignored.
  Use synthetic examples in documentation.

---

## Team

Per-member Git contribution is graded. Commit under your own account; do not batch other
people's work into your commits.
