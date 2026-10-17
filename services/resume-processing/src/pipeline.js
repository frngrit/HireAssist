// UC-2: screen one resume.
//   screening.requested  →  fetch PDF from MinIO  →  PDF to text
//   →  AI.ExtractProfileFromText  →  save profile (MongoDB)
//   →  AI.ScoreAgainstCriteria    →  save result  (MongoDB)
//   →  screening.completed
//
// Safe to run twice for the same (batch_id, resume_id): a finished result is
// returned again instead of being recomputed, and a half-finished one reuses
// its candidate id and profile (ADR-002: one independently retried unit of
// work per resume).

const ai = require("./ai");
const { fetchResumeFile, FileNotFoundError } = require("./storage");
const { extractText, UnparsableResumeError } = require("./pdf");
const { CandidateProfile, ScreeningResult, nextCandidateId } = require("./db");

// Retrying will not help: bad message, missing file, or an input the AI rejects.
class PermanentError extends Error {}

function validate(msg) {
  const missing = [];
  for (const field of ["batch_id", "resume_id"]) if (!msg?.[field]) missing.push(field);
  if (!msg?.file?.bucket || !msg?.file?.key) missing.push("file.bucket/file.key");
  if (!Array.isArray(msg?.criteria) || msg.criteria.length === 0) missing.push("criteria");
  if (missing.length) throw new PermanentError(`Invalid screening.requested message, missing: ${missing.join(", ")}`);
  for (const c of msg.criteria) {
    if (!c.id || !c.text) throw new PermanentError("Every criterion needs an id and a text");
  }
}

const toCriterion = (c) => ({
  id: String(c.id),
  text: String(c.text),
  must_have: Boolean(c.must_have),
  weight: Number(c.weight) || 0,
  category: c.category ? String(c.category) : "",
});

const toAiProfile = (doc) => ({
  candidate_id: doc._id,
  work_history: doc.workHistory || [],
  skills: doc.skills || [],
  education: doc.education || [],
  additional_text: doc.additionalText || "",
});

const attribution = (a) => (a ? { provider: a.provider, model: a.model, promptVersion: a.prompt_version } : undefined);

// The screening.completed message (see MESSAGES.md)
function toCompleted(result, correlationId) {
  return {
    batch_id: result.batchId,
    resume_id: result.resumeId,
    job_opening_id: result.jobOpeningId || null,
    candidate_id: result.candidateId || null,
    status: result.status,
    score: result.status === "scored" ? result.score : null,
    meets_must_haves: result.status === "scored" ? result.meetsMustHaves : null,
    unmet_must_have_ids: result.unmetMustHaveIds || [],
    justification: result.justification || null,
    error: result.error || null,
    correlation_id: correlationId || null,
  };
}

async function save(id, fields) {
  return ScreeningResult.findByIdAndUpdate(id, { ...fields, updatedAt: new Date() }, { new: true });
}

async function processScreeningRequest(msg) {
  validate(msg);
  const id = `${msg.batch_id}:${msg.resume_id}`;
  const criteria = msg.criteria.map(toCriterion);
  const language = ai.toLanguage(msg.language);

  // 1. Redelivery of finished work: answer again, do not redo it.
  let result = await ScreeningResult.findById(id);
  if (result && (result.status === "scored" || result.status === "unparsable")) {
    return { completed: toCompleted(result, msg.correlation_id), replayed: true };
  }

  // 2. Claim the work and keep the same candidate id across retries.
  const candidateId = result?.candidateId || (await nextCandidateId());
  if (!result) {
    result = await ScreeningResult.create({
      _id: id,
      batchId: msg.batch_id,
      resumeId: msg.resume_id,
      jobOpeningId: msg.job_opening_id,
      candidateId,
      status: "processing",
      criteria,
      attempts: 1,
    });
  } else {
    result = await save(id, { status: "processing", attempts: (result.attempts || 0) + 1, error: null });
  }

  // 3. Fetch the file and read its text.
  let text;
  try {
    const file = await fetchResumeFile(msg.file.bucket, msg.file.key);
    text = await extractText(file);
  } catch (err) {
    if (err instanceof UnparsableResumeError) {
      // FR-2.7: flag for manual review; retrying cannot fix a scanned image.
      result = await save(id, { status: "unparsable", error: err.message });
      return { completed: toCompleted(result, msg.correlation_id) };
    }
    if (err instanceof FileNotFoundError) throw new PermanentError(err.message);
    throw err; // storage outage: retry
  }

  try {
    // 4. Extract a structured profile once per candidate, then reuse it.
    let profile = await CandidateProfile.findById(candidateId);
    if (!profile || !profile.extraction?.model) {
      const extracted = await ai.extractProfileFromText({ candidate_id: candidateId, resume_text: text, language });
      const p = extracted.profile || {};
      profile = await CandidateProfile.findByIdAndUpdate(
        candidateId,
        {
          _id: candidateId,
          jobOpeningId: msg.job_opening_id,
          resumeText: text,
          workHistory: p.work_history || [],
          skills: p.skills || [],
          education: p.education || [],
          additionalText: p.additional_text || "",
          source: { batchId: msg.batch_id, resumeId: msg.resume_id, bucket: msg.file.bucket, key: msg.file.key },
          extraction: attribution(extracted.attribution),
        },
        { upsert: true, new: true }
      );
    }

    // 5. Score against the criteria snapshot that came with the request.
    const scored = await ai.scoreAgainstCriteria({ profile: toAiProfile(profile), criteria, language });
    result = await save(id, {
      status: "scored",
      score: scored.score,
      meetsMustHaves: scored.meets_must_haves,
      unmetMustHaveIds: scored.unmet_must_have_ids || [],
      assessments: (scored.assessments || []).map((a) => ({
        criterionId: a.criterion_id,
        met: a.met,
        score: a.score,
        evidence: a.evidence,
      })),
      justification: scored.justification,
      scoring: attribution(scored.attribution),
      error: null,
    });
    return { completed: toCompleted(result, msg.correlation_id) };
  } catch (err) {
    if (err.code !== undefined && !ai.isTransient(err)) {
      throw new PermanentError(`AI Service rejected the request: ${err.details || err.message}`);
    }
    if (err.code !== undefined) {
      throw new Error(`AI Service unavailable (gRPC ${err.code}): ${(err.details || "").split(".")[0]}`);
    }
    throw err; // AI Service down or slow: retry
  }
}

// Record a final failure so getCandidateProfile / dashboards can show it.
async function markFailed(msg, error) {
  if (!msg?.batch_id || !msg?.resume_id) return null;
  const id = `${msg.batch_id}:${msg.resume_id}`;
  const result = await ScreeningResult.findByIdAndUpdate(
    id,
    {
      _id: id,
      batchId: msg.batch_id,
      resumeId: msg.resume_id,
      jobOpeningId: msg.job_opening_id,
      status: "failed",
      error,
      updatedAt: new Date(),
    },
    { upsert: true, new: true }
  );
  return toCompleted(result, msg.correlation_id);
}

module.exports = { processScreeningRequest, markFailed, PermanentError, toCompleted };
