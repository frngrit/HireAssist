"""The prompts and the JSON schemas the model must answer in.

Versioned: PROMPT_VERSION travels with every answer in ModelAttribution, so a
justification a Recruiter saw months ago can be traced to the wording that
produced it (ADR-004). Change a prompt, change the version — an edited prompt
under an unchanged version makes past attributions lie.
"""

import copy
from typing import Any

PROMPT_VERSION = "v1"

_LANGUAGE_NAMES = {0: "English", 1: "Thai", 2: "English"}


def language_name(language: int) -> str:
    """Proto Language enum to a word the prompt can use. Unspecified is English."""
    return _LANGUAGE_NAMES.get(language, "English")


# ── Shared framing ──────────────────────────────────────────────────────────

_GROUNDING = (
    "You judge only what the resume states. You never infer a skill from a job "
    "title, never credit experience the text does not show, and never penalise "
    "a candidate for anything outside the stated criteria — not their name, "
    "their gender, their age, their nationality, the institution they attended "
    "or any gap in their dates. If the resume does not evidence a criterion, "
    "the criterion is not met, and you say so plainly."
)


# ── UC-1: derive criteria ───────────────────────────────────────────────────

CRITERIA_SYSTEM = (
    "You help a recruiter turn a free-text role description into a reviewable "
    "set of screening criteria.\n\n"
    "Each criterion must be one testable statement a reader could check against "
    "a resume — not a paragraph and not a wish. Mark a criterion must_have only "
    "when the description states it as a requirement; everything else is "
    "weighted, and the weights of the non-must-have criteria should sum to "
    "roughly 100. Weight is ignored for must-haves; give them 0.\n\n"
    "Anything in the description you cannot turn into a criterion goes in "
    "'uninterpreted', verbatim. Returning it there is better than guessing: the "
    "recruiter fills it in by hand. Everything you produce is a proposal a "
    "recruiter will review and correct."
)

CRITERIA_SCHEMA: dict[str, Any] = {
    "type": "object",
    "properties": {
        "proposed_title": {"type": "string"},
        "criteria": {
            "type": "array",
            "items": {
                "type": "object",
                "properties": {
                    "text": {"type": "string"},
                    "must_have": {"type": "boolean"},
                    "weight": {"type": "integer", "minimum": 0, "maximum": 100},
                    "category": {
                        "type": "string",
                        "description": "experience, skills, education, or other",
                    },
                },
                "required": ["text", "must_have", "weight"],
            },
        },
        "uninterpreted": {"type": "array", "items": {"type": "string"}},
    },
    "required": ["proposed_title", "criteria", "uninterpreted"],
}


def criteria_user(description: str, language: int) -> str:
    return (
        f"Write the title and the criteria in {language_name(language)}.\n\n"
        f"Role description:\n{description}"
    )


# ── UC-2: extract a profile from resume text ────────────────────────────────

EXTRACTION_SYSTEM = (
    "You turn the text of one resume into a structured profile.\n\n"
    "Transcribe, do not judge. Every field you fill must come from the text in "
    "front of you: you never infer a skill from a job title, never estimate a "
    "date that is not written, and never tidy a claim into something stronger "
    "than what it says. A resume in Thai stays in Thai; do not translate.\n\n"
    "Dates appear as written, in whatever form the resume uses. Leave the end "
    "of a current role empty rather than writing 'present'. A Thai resume may "
    "date its entries in the Buddhist era, 543 years ahead of the Gregorian "
    "one — convert those to the Gregorian year.\n\n"
    "Put anything real that does not fit the structured fields — certifications, "
    "publications, projects, languages spoken, a personal summary — into "
    "additional_text, in the resume's own words. Losing it is worse than "
    "leaving it unstructured, because the text is read when the candidate is "
    "assessed.\n\n"
    "**Leave out, from every field including additional_text: the candidate's "
    "name, their photograph or any description of it, contact details of any "
    "kind, gender, age or date of birth, marital or family status, religion, "
    "nationality, and national identification numbers.** These may not be used "
    "to assess anyone, so they must not appear in the profile at all. This "
    "holds even when the resume states them prominently, and even when a field "
    "would otherwise be empty.\n\n"
    "If the text is too garbled to structure — extraction failed, or the page "
    "is not a resume — return empty arrays and an empty additional_text rather "
    "than inventing plausible content. Nothing is a usable answer; a fabricated "
    "career is not."
)

PROFILE_SCHEMA: dict[str, Any] = {
    "type": "object",
    "properties": {
        "work_history": {
            "type": "array",
            "items": {
                "type": "object",
                "properties": {
                    "title": {"type": "string"},
                    "organisation": {"type": "string"},
                    "start": {"type": "string", "description": "as written in the resume"},
                    "end": {"type": "string", "description": "empty when this is current"},
                    "summary": {"type": "string"},
                },
                "required": ["title", "organisation", "start", "end", "summary"],
            },
        },
        "skills": {"type": "array", "items": {"type": "string"}},
        "education": {
            "type": "array",
            "items": {
                "type": "object",
                "properties": {
                    "qualification": {"type": "string"},
                    "institution": {"type": "string"},
                    "completed": {"type": "string"},
                },
                "required": ["qualification", "institution", "completed"],
            },
        },
        "additional_text": {
            "type": "string",
            "description": "Resume content that fits no field above, in the resume's own "
            "words, excluding every attribute the instructions bar.",
        },
    },
    "required": ["work_history", "skills", "education", "additional_text"],
}


def extraction_user(resume_text: str, language: int) -> str:
    return (
        f"The resume is written in {language_name(language)}; keep its wording.\n\n"
        f"Resume text:\n{resume_text}"
    )


# ── UC-2: score against criteria ────────────────────────────────────────────

SCORING_SYSTEM = (
    "You assess one candidate's resume against a fixed list of screening "
    f"criteria.\n\n{_GROUNDING}\n\n"
    "Return exactly one assessment for every criterion you are given, "
    "identified by the criterion id given to you, and no assessment for any "
    "other id. For each one: whether the resume meets it, a 0-100 score for how "
    "well, and the words from the resume that judgement rests on. Quote the "
    "resume for evidence; when there is nothing to quote, say what is missing.\n\n"
    "Do not compute an overall score — that is done from your assessments.\n\n"
    "Finally, write a short justification a recruiter can read on its own: what "
    "this candidate demonstrably has, what they are missing against the "
    "must-haves, and nothing that is not in your assessments."
)

SCORING_SCHEMA: dict[str, Any] = {
    "type": "object",
    "properties": {
        "assessments": {
            "type": "array",
            "items": {
                "type": "object",
                "properties": {
                    "criterion_id": {"type": "string"},
                    "met": {"type": "boolean"},
                    "score": {"type": "integer", "minimum": 0, "maximum": 100},
                    "evidence": {"type": "string"},
                },
                "required": ["criterion_id", "met", "score", "evidence"],
            },
        },
        "justification": {"type": "string"},
    },
    "required": ["assessments", "justification"],
}


def scoring_user(profile_text: str, criteria_text: str, language: int) -> str:
    return (
        f"Write the evidence and the justification in {language_name(language)}.\n\n"
        f"Criteria:\n{criteria_text}\n\n"
        f"Candidate resume:\n{profile_text}"
    )


# ── UC-3: generate interview questions ──────────────────────────────────────

QUESTIONS_SYSTEM = (
    "You prepare interview questions for a recruiter about to interview one "
    f"candidate for one role.\n\n{_GROUNDING}\n\n"
    "Every question tests a criterion and comes from the resume: either it "
    "probes a specific claim the candidate makes ('resume_claim'), or it "
    "addresses a must-have the resume did not evidence ('must_have_gap'), or it "
    "goes deeper into the role's stated requirements ('role_depth'). Name the "
    "criterion id each question tests, and quote the resume text or state the "
    "gap it came from.\n\n"
    "Ask open questions that make the candidate describe what they actually "
    "did. Nothing generic, nothing answerable yes or no, and nothing that "
    "touches a protected characteristic. Prefer six to ten questions, weighted "
    "towards the must-haves."
)

QUESTIONS_SCHEMA: dict[str, Any] = {
    "type": "object",
    "properties": {
        "questions": {
            "type": "array",
            "items": {
                "type": "object",
                "properties": {
                    "text": {"type": "string"},
                    "criterion_id": {"type": "string"},
                    "source_evidence": {"type": "string"},
                    "kind": {
                        "type": "string",
                        "enum": ["resume_claim", "must_have_gap", "role_depth"],
                    },
                },
                "required": ["text", "criterion_id", "source_evidence", "kind"],
            },
        }
    },
    "required": ["questions"],
}


def questions_schema(criterion_ids: list[str]) -> dict[str, Any]:
    """QUESTIONS_SCHEMA with ``criterion_id`` limited to the ids supplied.

    The model then picks from the role's criteria rather than writing an id of
    its own; the handler still drops any question that strays.
    """
    schema = copy.deepcopy(QUESTIONS_SCHEMA)
    item = schema["properties"]["questions"]["items"]
    item["properties"]["criterion_id"] = {"type": "string", "enum": list(criterion_ids)}
    return schema


def questions_user(profile_text: str, criteria_text: str, emphasis: str, language: int) -> str:
    steer = f"\n\nThe recruiter asked to emphasise: {emphasis}" if emphasis else ""
    return (
        f"Write the questions in {language_name(language)}.\n\n"
        f"Criteria:\n{criteria_text}\n\n"
        f"Candidate resume:\n{profile_text}{steer}"
    )
