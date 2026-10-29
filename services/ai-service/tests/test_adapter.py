"""The adapter: what the handlers do with an answer, and what they refuse.

The model's answer is the input to these tests, not the thing under test, so
every one of them runs against a scripted provider over a real channel.
"""

import grpc
import pytest

from ai_service.answers import Assessment, weighted_total
from ai_service.provider import ProviderError

CRITERIA_TOOL = "proposed_criteria"
SCORING_TOOL = "criterion_assessments"
QUESTIONS_TOOL = "interview_questions"


def scoring_request(*criteria):
    from ai_service.gen.hireassist.ai.v1 import ai_service_pb2 as pb

    return pb.ScoreAgainstCriteriaRequest(
        profile=pb.CandidateProfile(
            candidate_id="c-1",
            skills=["Go", "PostgreSQL"],
            work_history=[
                pb.WorkExperience(
                    title="Backend Engineer",
                    organisation="Acme",
                    start="2021",
                    end="",
                    summary="Built payment services in Go.",
                )
            ],
            education=[pb.Education(qualification="BSc Computer Science", institution="CU")],
        ),
        criteria=list(criteria),
        language=pb.LANGUAGE_ENGLISH,
    )


def criterion(cid, text, *, must_have=False, weight=0):
    from ai_service.gen.hireassist.ai.v1 import ai_service_pb2 as pb

    return pb.Criterion(id=cid, text=text, must_have=must_have, weight=weight)


# ── the weighted total is ours, not the model's ─────────────────────────────


def test_total_is_weighted_by_criterion_weight():
    assessments = [
        Assessment(criterion_id="a", met=True, score=100),
        Assessment(criterion_id="b", met=False, score=0),
    ]
    assert weighted_total(assessments, {"a": 75, "b": 25}) == 75
    assert weighted_total(assessments, {"a": 25, "b": 75}) == 25


def test_must_haves_carry_no_weight_in_the_total():
    """They gate the candidate instead, so a met must-have cannot inflate a
    weak score and an unmet one cannot sink an otherwise strong candidate
    twice."""
    assessments = [
        Assessment(criterion_id="gate", met=True, score=100),
        Assessment(criterion_id="w", met=False, score=40),
    ]
    assert weighted_total(assessments, {"gate": 0, "w": 100}) == 40


def test_an_all_must_have_opening_still_gets_a_total():
    assessments = [
        Assessment(criterion_id="a", met=True, score=90),
        Assessment(criterion_id="b", met=True, score=70),
    ]
    assert weighted_total(assessments, {"a": 0, "b": 0}) == 80


# ── scoring end to end ──────────────────────────────────────────────────────


def test_scoring_maps_a_good_answer_onto_the_response(make_stub):
    stub, provider = make_stub(
        answers={
            SCORING_TOOL: {
                "assessments": [
                    {
                        "criterion_id": "cr-1",
                        "met": True,
                        "score": 90,
                        "evidence": "Built payment services in Go.",
                    },
                    {"criterion_id": "cr-2", "met": False, "score": 20, "evidence": "No Kafka."},
                ],
                "justification": "Strong Go background; no evidence of streaming work.",
            }
        }
    )
    response = stub.ScoreAgainstCriteria(
        scoring_request(
            criterion("cr-1", "Production Go", must_have=True),
            criterion("cr-2", "Kafka", weight=100),
        )
    )

    assert response.score == 20  # cr-1 is a gate and carries no weight
    assert response.meets_must_haves is True
    assert list(response.unmet_must_have_ids) == []
    assert response.justification.startswith("Strong Go")
    assert response.attribution.model == "fake-model-1"
    assert len(response.assessments) == 2

    # The resume reached the prompt as parsed fields, not as a blob.
    sent = provider.calls[0]["user"]
    assert "Backend Engineer at Acme" in sent
    assert "id=cr-1 (MUST HAVE)" in sent
    assert "id=cr-2 (weight 100)" in sent


def test_an_unmet_must_have_disqualifies_and_is_named(make_stub):
    stub, _ = make_stub(
        answers={
            SCORING_TOOL: {
                "assessments": [
                    {"criterion_id": "cr-1", "met": False, "score": 10, "evidence": "None."},
                    {
                        "criterion_id": "cr-2",
                        "met": True,
                        "score": 80,
                        "evidence": "Kafka at Acme.",
                    },
                ],
                "justification": "Missing the required Go experience.",
            }
        }
    )
    response = stub.ScoreAgainstCriteria(
        scoring_request(
            criterion("cr-1", "Production Go", must_have=True),
            criterion("cr-2", "Kafka", weight=100),
        )
    )
    assert response.meets_must_haves is False
    assert list(response.unmet_must_have_ids) == ["cr-1"]
    assert response.score == 80  # the gate is reported, not folded into the number


@pytest.mark.parametrize(
    "assessments",
    [
        pytest.param(
            [{"criterion_id": "cr-1", "met": True, "score": 90, "evidence": "x"}],
            id="missing-a-criterion",
        ),
        pytest.param(
            [
                {"criterion_id": "cr-1", "met": True, "score": 90, "evidence": "x"},
                {"criterion_id": "cr-2", "met": True, "score": 90, "evidence": "x"},
                {"criterion_id": "cr-9", "met": True, "score": 90, "evidence": "x"},
            ],
            id="invented-a-criterion",
        ),
    ],
)
def test_assessments_must_match_the_criteria_exactly(make_stub, assessments):
    """A missing assessment silently lowers the total and an invented one
    raises it. Neither is visible in the result, so neither is accepted."""
    stub, _ = make_stub(answers={SCORING_TOOL: {"assessments": assessments, "justification": "ok"}})
    with pytest.raises(grpc.RpcError) as exc:
        stub.ScoreAgainstCriteria(
            scoring_request(
                criterion("cr-1", "Production Go", must_have=True),
                criterion("cr-2", "Kafka", weight=100),
            )
        )
    assert exc.value.code() == grpc.StatusCode.INTERNAL


def test_a_score_without_a_justification_is_refused(make_stub):
    stub, _ = make_stub(
        answers={
            SCORING_TOOL: {
                "assessments": [
                    {"criterion_id": "cr-1", "met": True, "score": 90, "evidence": "Go at Acme."}
                ],
                "justification": "",
            }
        }
    )
    with pytest.raises(grpc.RpcError) as exc:
        stub.ScoreAgainstCriteria(scoring_request(criterion("cr-1", "Go", weight=100)))
    assert exc.value.code() == grpc.StatusCode.INTERNAL


def test_a_provider_outage_is_retryable_not_internal(make_stub):
    stub, _ = make_stub(answers={SCORING_TOOL: ProviderError("connection reset")})
    with pytest.raises(grpc.RpcError) as exc:
        stub.ScoreAgainstCriteria(scoring_request(criterion("cr-1", "Go", weight=100)))
    assert exc.value.code() == grpc.StatusCode.UNAVAILABLE


def test_the_vendor_never_appears_in_the_status(make_stub):
    """The caller learns the provider is unavailable, never which provider."""
    stub, _ = make_stub(answers={SCORING_TOOL: ProviderError("connection reset")})
    with pytest.raises(grpc.RpcError) as exc:
        stub.ScoreAgainstCriteria(scoring_request(criterion("cr-1", "Go", weight=100)))
    assert "anthropic" not in exc.value.details().lower()


# ── deriving criteria ───────────────────────────────────────────────────────


def test_derived_criteria_get_ids_from_us_not_the_model(make_stub):
    from ai_service.gen.hireassist.ai.v1 import ai_service_pb2 as pb

    stub, _ = make_stub(
        answers={
            CRITERIA_TOOL: {
                "proposed_title": "Backend Engineer",
                "criteria": [
                    {"text": "3 years Go", "must_have": True, "weight": 50, "category": "skills"},
                    {"text": "Payments domain", "must_have": False, "weight": 60},
                ],
                "uninterpreted": ["must be a team player"],
            }
        }
    )
    response = stub.DeriveCriteriaFromDescription(
        pb.DeriveCriteriaFromDescriptionRequest(
            description="We need a backend engineer with 3 years of Go.",
            language=pb.LANGUAGE_ENGLISH,
        )
    )
    assert response.proposed_title == "Backend Engineer"
    assert [c.id for c in response.criteria] == ["proposed-1", "proposed-2"]
    # A weight on a must-have is dropped: the proto says it is ignored, so
    # carrying it would invite a caller to use it.
    assert response.criteria[0].weight == 0
    assert response.criteria[1].weight == 60
    assert list(response.uninterpreted) == ["must be a team player"]


def test_an_empty_description_is_rejected_before_the_model(make_stub):
    from ai_service.gen.hireassist.ai.v1 import ai_service_pb2 as pb

    stub, provider = make_stub(answers={})
    with pytest.raises(grpc.RpcError) as exc:
        stub.DeriveCriteriaFromDescription(
            pb.DeriveCriteriaFromDescriptionRequest(description="   ")
        )
    assert exc.value.code() == grpc.StatusCode.INVALID_ARGUMENT
    assert provider.calls == []


# ── interview questions ─────────────────────────────────────────────────────


def test_questions_carry_their_criterion_and_kind(make_stub):
    from ai_service.gen.hireassist.ai.v1 import ai_service_pb2 as pb

    stub, provider = make_stub(
        answers={
            QUESTIONS_TOOL: {
                "questions": [
                    {
                        "text": "Walk me through the payment service you built at Acme.",
                        "criterion_id": "cr-1",
                        "source_evidence": "Built payment services in Go.",
                        "kind": "resume_claim",
                    },
                    {
                        "text": "How have you handled streaming data?",
                        "criterion_id": "cr-2",
                        "source_evidence": "No Kafka on the resume.",
                        "kind": "must_have_gap",
                    },
                ]
            }
        }
    )
    request = pb.GenerateInterviewQuestionsRequest(
        profile=pb.CandidateProfile(candidate_id="c-1", skills=["Go"]),
        criteria=[criterion("cr-1", "Production Go"), criterion("cr-2", "Kafka", must_have=True)],
        emphasis="more system design",
        language=pb.LANGUAGE_ENGLISH,
    )
    response = stub.GenerateInterviewQuestions(request)

    assert len(response.questions) == 2
    assert response.questions[0].kind == pb.QUESTION_KIND_RESUME_CLAIM
    assert response.questions[1].kind == pb.QUESTION_KIND_MUST_HAVE_GAP
    assert response.questions[1].criterion_id == "cr-2"
    assert "more system design" in provider.calls[0]["user"]


def test_a_question_citing_an_unknown_criterion_is_refused(make_stub):
    """An interviewer who cannot trace a question to a criterion cannot
    justify asking it."""
    from ai_service.gen.hireassist.ai.v1 import ai_service_pb2 as pb

    stub, _ = make_stub(
        answers={
            QUESTIONS_TOOL: {
                "questions": [
                    {
                        "text": "Where do you see yourself in five years?",
                        "criterion_id": "cr-invented",
                        "source_evidence": "",
                        "kind": "role_depth",
                    }
                ]
            }
        }
    )
    request = pb.GenerateInterviewQuestionsRequest(
        profile=pb.CandidateProfile(candidate_id="c-1"),
        criteria=[criterion("cr-1", "Production Go")],
    )
    with pytest.raises(grpc.RpcError) as exc:
        stub.GenerateInterviewQuestions(request)
    assert exc.value.code() == grpc.StatusCode.INTERNAL


# ── language ────────────────────────────────────────────────────────────────


def test_thai_is_asked_for_in_the_prompt(make_stub):
    from ai_service.gen.hireassist.ai.v1 import ai_service_pb2 as pb

    stub, provider = make_stub(
        answers={
            SCORING_TOOL: {
                "assessments": [
                    {"criterion_id": "cr-1", "met": True, "score": 80, "evidence": "Go."}
                ],
                "justification": "เหมาะสม",
            }
        }
    )
    request = scoring_request(criterion("cr-1", "Production Go", weight=100))
    request.language = pb.LANGUAGE_THAI
    response = stub.ScoreAgainstCriteria(request)

    assert "in Thai" in provider.calls[0]["user"]
    assert response.justification == "เหมาะสม"


def test_only_the_question_citing_an_unknown_criterion_is_dropped(make_stub):
    """A model once put a question kind where the criterion id belongs. The
    stray question goes; the rest of the guide survives."""
    from ai_service.gen.hireassist.ai.v1 import ai_service_pb2 as pb

    stub, provider = make_stub(
        answers={
            QUESTIONS_TOOL: {
                "questions": [
                    {
                        "text": "Walk me through the Go service you built.",
                        "criterion_id": "cr-1",
                        "source_evidence": "Built payment services in Go.",
                        "kind": "resume_claim",
                    },
                    {
                        "text": "How would you design for failure?",
                        "criterion_id": "role_depth",
                        "source_evidence": "",
                        "kind": "role_depth",
                    },
                ]
            }
        }
    )
    request = pb.GenerateInterviewQuestionsRequest(
        profile=pb.CandidateProfile(candidate_id="c-1"),
        criteria=[criterion("cr-1", "Production Go")],
    )
    response = stub.GenerateInterviewQuestions(request)

    assert [q.criterion_id for q in response.questions] == ["cr-1"]
    # The model was told which ids exist.
    item = provider.calls[0]["schema"]["properties"]["questions"]["items"]
    assert item["properties"]["criterion_id"]["enum"] == ["cr-1"]
