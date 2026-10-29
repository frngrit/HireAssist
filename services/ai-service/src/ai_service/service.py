"""The three RPC handlers.

Each one takes a domain request and returns a domain answer. Nothing here
exposes a prompt, a token count or a provider to the caller — that is the
boundary this service exists to hold (ADR-004).

Every handler follows the same four steps: render the request into a prompt,
ask the provider for an answer conforming to a schema, validate that answer,
and map it into the proto response. A step that cannot complete aborts with a
status. None of them degrades: an empty score would rank a candidate last,
which is the failure the product exists to prevent, so a bad answer is an
error and the caller's retry-then-needs_manual_review path (ADR-002) handles
it.

Status codes the caller can act on:

    FAILED_PRECONDITION  no model credential is configured — retrying is futile
    INVALID_ARGUMENT     the request is unusable, e.g. scoring with no criteria
    UNAVAILABLE          the provider could not be reached or refused — retry
    INTERNAL             the model answered, but not usably — worth one retry
"""

import logging
import time

import grpc
from pydantic import ValidationError

from ai_service import prompts
from ai_service.answers import (
    DerivedCriteria,
    ExtractedProfile,
    Questions,
    Scoring,
    weighted_total,
)
from ai_service.attribution import attribution_for
from ai_service.config import Settings, settings
from ai_service.provider import Provider, ProviderError

log = logging.getLogger(__name__)

_QUESTION_KINDS = {
    "resume_claim": "QUESTION_KIND_RESUME_CLAIM",
    "must_have_gap": "QUESTION_KIND_MUST_HAVE_GAP",
    "role_depth": "QUESTION_KIND_ROLE_DEPTH",
}


class AiService:
    """Implements hireassist.ai.v1.AiService."""

    def __init__(self, config: Settings | None = None, provider: Provider | None = None) -> None:
        self.settings = config or settings
        self.provider = provider

    # ── UC-1 ────────────────────────────────────────────────────────────────
    def DeriveCriteriaFromDescription(self, request, context):  # noqa: N802
        from ai_service.gen.hireassist.ai.v1 import ai_service_pb2 as pb

        log.info("DeriveCriteriaFromDescription language=%s", request.language)
        provider = self._provider(context)

        if not request.description.strip():
            context.abort(
                grpc.StatusCode.INVALID_ARGUMENT,
                "no description supplied; there is nothing to derive criteria from",
            )

        answer = self._ask(
            context,
            provider,
            system=prompts.CRITERIA_SYSTEM,
            user=prompts.criteria_user(request.description, request.language),
            schema=prompts.CRITERIA_SCHEMA,
            schema_name="proposed_criteria",
            schema_description="The proposed job title and screening criteria.",
            model=DerivedCriteria,
        )

        # Ids are assigned here, not by the model: the Hiring Service owns
        # criterion identity, and these are placeholders it replaces when the
        # Recruiter accepts the proposal.
        criteria = [
            pb.Criterion(
                id=f"proposed-{index}",
                text=criterion.text,
                must_have=criterion.must_have,
                weight=0 if criterion.must_have else criterion.weight,
                category=criterion.category,
            )
            for index, criterion in enumerate(answer.parsed.criteria, start=1)
        ]
        return pb.DeriveCriteriaFromDescriptionResponse(
            proposed_title=answer.parsed.proposed_title,
            criteria=criteria,
            uninterpreted=answer.parsed.uninterpreted,
            attribution=attribution_for(self.settings, answer.model),
        )

    # ── UC-2, before scoring ────────────────────────────────────────────────
    def ExtractProfileFromText(self, request, context):  # noqa: N802
        from ai_service.gen.hireassist.ai.v1 import ai_service_pb2 as pb

        log.info(
            "ExtractProfileFromText candidate=%s chars=%d",
            request.candidate_id,
            len(request.resume_text),
        )
        provider = self._provider(context)

        if not request.candidate_id.strip():
            # Candidate identity belongs to the caller. Inventing one here
            # would produce a profile nothing can be matched back to.
            context.abort(
                grpc.StatusCode.INVALID_ARGUMENT,
                "no candidate_id supplied; this service does not assign candidate identity",
            )
        if not request.resume_text.strip():
            context.abort(
                grpc.StatusCode.INVALID_ARGUMENT,
                "no resume text supplied; there is nothing to extract a profile from",
            )

        answer = self._ask(
            context,
            provider,
            system=prompts.EXTRACTION_SYSTEM,
            user=prompts.extraction_user(request.resume_text, request.language),
            schema=prompts.PROFILE_SCHEMA,
            schema_name="candidate_profile",
            schema_description="The resume's work history, skills and education, structured.",
            model=ExtractedProfile,
        )

        if answer.parsed.is_empty():
            # Empty in every field at once is a failed extraction, not a sparse
            # resume. Returning it would hand scoring a candidate with no
            # evidence for anything, which scores last for the wrong reason.
            context.abort(
                grpc.StatusCode.INTERNAL,
                "nothing could be extracted from the resume text; it is unusable "
                "and the resume needs manual review",
            )

        profile = pb.CandidateProfile(
            candidate_id=request.candidate_id,
            work_history=[
                pb.WorkExperience(
                    title=job.title,
                    organisation=job.organisation,
                    start=job.start,
                    end=job.end,
                    summary=job.summary,
                )
                for job in answer.parsed.work_history
            ],
            skills=answer.parsed.skills,
            education=[
                pb.Education(
                    qualification=item.qualification,
                    institution=item.institution,
                    completed=item.completed,
                )
                for item in answer.parsed.education
            ],
            additional_text=answer.parsed.additional_text,
        )
        return pb.ExtractProfileFromTextResponse(
            profile=profile,
            attribution=attribution_for(self.settings, answer.model),
        )

    # ── UC-2 ────────────────────────────────────────────────────────────────
    def ScoreAgainstCriteria(self, request, context):  # noqa: N802
        from ai_service.gen.hireassist.ai.v1 import ai_service_pb2 as pb

        log.info(
            "ScoreAgainstCriteria candidate=%s criteria=%d",
            request.profile.candidate_id,
            len(request.criteria),
        )
        provider = self._provider(context)

        if not request.criteria:
            context.abort(
                grpc.StatusCode.INVALID_ARGUMENT,
                "no criteria supplied; a score is only defensible against stated criteria",
            )

        answer = self._ask(
            context,
            provider,
            system=prompts.SCORING_SYSTEM,
            user=prompts.scoring_user(
                _render_profile(request.profile),
                _render_criteria(request.criteria),
                request.language,
            ),
            schema=prompts.SCORING_SCHEMA,
            schema_name="criterion_assessments",
            schema_description="One assessment per criterion, and a justification.",
            model=Scoring,
        )

        requested = {c.id: c for c in request.criteria}
        assessed = {a.criterion_id: a for a in answer.parsed.assessments}
        # Strict both ways. A missing assessment silently lowers the weighted
        # total and an invented id silently raises it; neither is visible in
        # the result, so neither is tolerated here.
        if assessed.keys() != requested.keys():
            missing = sorted(requested.keys() - assessed.keys())
            unknown = sorted(assessed.keys() - requested.keys())
            context.abort(
                grpc.StatusCode.INTERNAL,
                f"assessments do not match the criteria supplied "
                f"(missing={missing}, unknown={unknown})",
            )
        if len(answer.parsed.assessments) != len(assessed):
            context.abort(
                grpc.StatusCode.INTERNAL, "more than one assessment for the same criterion"
            )

        unmet_must_haves = [
            cid for cid, c in requested.items() if c.must_have and not assessed[cid].met
        ]
        weights = {c.id: 0 if c.must_have else c.weight for c in request.criteria}

        return pb.ScoreAgainstCriteriaResponse(
            score=weighted_total(answer.parsed.assessments, weights),
            meets_must_haves=not unmet_must_haves,
            unmet_must_have_ids=sorted(unmet_must_haves),
            assessments=[
                pb.CriterionAssessment(
                    criterion_id=a.criterion_id, met=a.met, score=a.score, evidence=a.evidence
                )
                for a in answer.parsed.assessments
            ],
            justification=answer.parsed.justification,
            attribution=attribution_for(self.settings, answer.model),
        )

    # ── UC-3 ────────────────────────────────────────────────────────────────
    def GenerateInterviewQuestions(self, request, context):  # noqa: N802
        from ai_service.gen.hireassist.ai.v1 import ai_service_pb2 as pb

        log.info("GenerateInterviewQuestions candidate=%s", request.profile.candidate_id)
        provider = self._provider(context)

        if not request.criteria:
            context.abort(
                grpc.StatusCode.INVALID_ARGUMENT,
                "no criteria supplied; questions are generated against the role's criteria",
            )

        answer = self._ask(
            context,
            provider,
            system=prompts.QUESTIONS_SYSTEM,
            user=prompts.questions_user(
                _render_profile(request.profile),
                _render_criteria(request.criteria),
                request.emphasis,
                request.language,
            ),
            schema=prompts.questions_schema([c.id for c in request.criteria]),
            schema_name="interview_questions",
            schema_description="Interview questions, each tied to a criterion.",
            model=Questions,
        )

        # A question the interviewer cannot trace to a criterion is a question
        # they cannot justify asking, so it is left out. The schema already
        # lists the supplied ids, but a model can still stray — one has put a
        # question kind where the id belongs. Dropping that question keeps the
        # rest of a good guide; only an answer with nothing traceable fails.
        known = {c.id for c in request.criteria}
        questions = [q for q in answer.parsed.questions if q.criterion_id in known]
        unknown = sorted({q.criterion_id for q in answer.parsed.questions} - known)
        if unknown:
            log.warning("dropped questions citing criteria that were not supplied: %s", unknown)
        if not questions:
            context.abort(
                grpc.StatusCode.INTERNAL,
                f"questions cite criteria that were not supplied: {unknown}",
            )

        return pb.GenerateInterviewQuestionsResponse(
            questions=[
                pb.InterviewQuestion(
                    text=q.text,
                    criterion_id=q.criterion_id,
                    source_evidence=q.source_evidence,
                    kind=pb.QuestionKind.Value(
                        _QUESTION_KINDS.get(q.kind, "QUESTION_KIND_UNSPECIFIED")
                    ),
                )
                for q in questions
            ],
            attribution=attribution_for(self.settings, answer.model),
        )

    # ── shared ──────────────────────────────────────────────────────────────

    def _provider(self, context) -> Provider:
        if self.provider is None:
            context.abort(
                grpc.StatusCode.FAILED_PRECONDITION,
                "no model credential is configured; set AI_SERVICE_API_KEY",
            )
        return self.provider

    def _ask(
        self, context, provider, *, system, user, schema, schema_name, schema_description, model
    ):
        """Ask the provider and validate the answer, or abort with a status."""
        started = time.monotonic()
        try:
            raw = provider.json_answer(
                system=system,
                user=user,
                schema=schema,
                schema_name=schema_name,
                schema_description=schema_description,
            )
        except ProviderError as exc:
            log.warning(
                "provider failed for %s after %dms: %s",
                schema_name,
                _ms_since(started),
                exc,
            )
            context.abort(grpc.StatusCode.UNAVAILABLE, f"model provider unavailable: {exc}")

        try:
            parsed = model.model_validate(raw.data)
        except ValidationError as exc:
            log.warning("unusable %s answer: %s", schema_name, exc)
            context.abort(
                grpc.StatusCode.INTERNAL,
                f"the model's answer did not satisfy the {schema_name} contract: "
                f"{exc.error_count()} problem(s)",
            )

        # One line per answered call: what it cost and how long it took. This
        # is the only place token usage is observable — the caller is told a
        # domain answer and nothing about the model that produced it.
        log.info(
            "%s answered by %s in %dms (in=%d out=%d tokens)",
            schema_name,
            raw.model,
            _ms_since(started),
            raw.input_tokens,
            raw.output_tokens,
        )
        return _Answer(parsed=parsed, model=raw.model)


class _Answer:
    __slots__ = ("parsed", "model")

    def __init__(self, parsed, model: str) -> None:
        self.parsed = parsed
        self.model = model


def _ms_since(started: float) -> int:
    return int((time.monotonic() - started) * 1000)


def _render_criteria(criteria) -> str:
    lines = []
    for c in criteria:
        kind = "MUST HAVE" if c.must_have else f"weight {c.weight}"
        category = f" [{c.category}]" if c.category else ""
        lines.append(f"- id={c.id} ({kind}){category}: {c.text}")
    return "\n".join(lines)


def _render_profile(profile) -> str:
    """The resume as the model sees it.

    Only the parsed profile — this service never receives a resume file, and
    never sees the candidate's name or contact details, because nothing in
    CandidateProfile carries them.
    """
    parts: list[str] = []

    if profile.work_history:
        parts.append("Work history:")
        for job in profile.work_history:
            dates = f"{job.start} to {job.end or 'present'}".strip()
            parts.append(f"- {job.title} at {job.organisation} ({dates})")
            if job.summary:
                parts.append(f"  {job.summary}")

    if profile.skills:
        parts.append("Skills: " + ", ".join(profile.skills))

    if profile.education:
        parts.append("Education:")
        for item in profile.education:
            parts.append(
                f"- {item.qualification}, {item.institution} ({item.completed or 'no date'})"
            )

    if profile.additional_text:
        parts.append("Other resume text:\n" + profile.additional_text)

    return "\n".join(parts) if parts else "(the parsed resume is empty)"
