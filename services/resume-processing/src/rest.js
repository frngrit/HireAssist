// REST boundary (contract: openapi.yaml). Per ADR-007 everything except the
// AI Service speaks REST.

const crypto = require("crypto");
const express = require("express");
const log = require("./log");
const db = require("./db");
const queue = require("./queue");

const { CandidateProfile, ScreeningResult } = db;
const NOT_FOUND = { error: "Candidate profile not found" };

function profileOut(doc) {
  return {
    id: doc._id,
    job_opening_id: doc.jobOpeningId || null,
    full_name: doc.fullName || null,
    email: doc.email || null,
    skills: doc.skills || [],
    work_history: doc.workHistory || [],
    education: doc.education || [],
    additional_text: doc.additionalText || "",
    resume_text: doc.resumeText || "",
    source: doc.source?.batchId ? doc.source : null,
    created_at: doc.createdAt.toISOString(),
  };
}

function screeningOut(r) {
  return {
    batch_id: r.batchId,
    resume_id: r.resumeId,
    job_opening_id: r.jobOpeningId || null,
    candidate_id: r.candidateId || null,
    status: r.status,
    score: r.score ?? null,
    meets_must_haves: r.meetsMustHaves ?? null,
    unmet_must_have_ids: r.unmetMustHaveIds || [],
    assessments: (r.assessments || []).map((a) => ({
      criterion_id: a.criterionId,
      met: a.met,
      score: a.score,
      evidence: a.evidence,
    })),
    justification: r.justification || null,
    error: r.error || null,
    attempts: r.attempts || 0,
    updated_at: (r.updatedAt || r.createdAt).toISOString(),
  };
}

function createApp() {
  const app = express();
  app.use(express.json());

  // Correlation id on every call (ADR-005) and one log line per request.
  app.use((req, res, next) => {
    const correlationId = req.get("X-Correlation-Id") || crypto.randomUUID();
    res.set("X-Correlation-Id", correlationId);
    const started = Date.now();
    res.on("finish", () => {
      if (req.path === "/healthz" || req.path === "/readyz") return;
      log.info("request", {
        correlation_id: correlationId,
        method: req.method,
        path: req.originalUrl,
        status: res.statusCode,
        duration_ms: Date.now() - started,
      });
    });
    next();
  });

  const handle = (fn) => async (req, res) => {
    try {
      await fn(req, res);
    } catch (err) {
      if (err.name === "CastError") return res.status(404).json(NOT_FOUND);
      if (err.name === "ValidationError") return res.status(400).json({ error: err.message });
      log.error("request failed", { path: req.originalUrl, error: err.message });
      res.status(500).json({ error: err.message });
    }
  };

  // ---- Health (ADR-001, ADR-005) ----
  app.get("/healthz", (req, res) => res.json({ status: "ok" }));
  app.get("/readyz", (req, res) => {
    const checks = { mongodb: db.isConnected(), rabbitmq: queue.isReady() };
    const ready = checks.mongodb && checks.rabbitmq;
    res.status(ready ? 200 : 503).json({ status: ready ? "ready" : "not ready", checks });
  });

  // ---- getCandidateProfile(): the profile plus every screening of it (UC-2 justification, UC-3) ----
  app.get("/candidates/:id", handle(async (req, res) => {
    const doc = await CandidateProfile.findById(req.params.id);
    if (!doc) return res.status(404).json(NOT_FOUND);
    const screenings = await ScreeningResult.find({ candidateId: doc._id }).sort({ updatedAt: -1 });
    res.json({ ...profileOut(doc), screenings: screenings.map(screeningOut) });
  }));

  // ---- listCandidateProfiles() ----
  app.get("/candidates", handle(async (req, res) => {
    const filter = {};
    if (req.query.jobOpeningId) filter.jobOpeningId = req.query.jobOpeningId;
    const docs = await CandidateProfile.find(filter).sort({ createdAt: -1 });
    res.json({ profiles: docs.map(profileOut) });
  }));

  // ---- Screening results of a batch (for demos and debugging) ----
  app.get("/screenings", handle(async (req, res) => {
    if (!req.query.batchId) return res.status(400).json({ error: "batchId is required" });
    const results = await ScreeningResult.find({ batchId: req.query.batchId }).sort({ score: -1 });
    res.json({ batch_id: req.query.batchId, results: results.map(screeningOut) });
  }));

  // ---- Manual CRUD kept from the REST CRUD milestone ----
  app.post("/candidates", handle(async (req, res) => {
    const { job_opening_id, full_name, email, skills, resume_text } = req.body;
    if (!full_name || !email) {
      return res.status(400).json({ error: "full_name and email are required" });
    }
    const doc = await CandidateProfile.create({
      _id: await db.nextCandidateId(),
      jobOpeningId: job_opening_id,
      fullName: full_name,
      email,
      skills,
      resumeText: resume_text,
    });
    res.status(201).json(profileOut(doc));
  }));

  app.put("/candidates/:id", handle(async (req, res) => {
    const { full_name, email, skills, resume_text } = req.body;
    const changes = {};
    if (full_name !== undefined) changes.fullName = full_name;
    if (email !== undefined) changes.email = email;
    if (skills !== undefined) changes.skills = skills;
    if (resume_text !== undefined) changes.resumeText = resume_text;
    const doc = await CandidateProfile.findByIdAndUpdate(req.params.id, changes, { new: true });
    if (!doc) return res.status(404).json(NOT_FOUND);
    res.json(profileOut(doc));
  }));

  // eraseCandidateProfile(): the profile and its derived screening documents
  app.delete("/candidates/:id", handle(async (req, res) => {
    const doc = await CandidateProfile.findByIdAndDelete(req.params.id);
    if (!doc) return res.status(404).json(NOT_FOUND);
    await ScreeningResult.deleteMany({ candidateId: req.params.id });
    res.json({ success: true });
  }));

  return app;
}

module.exports = { createApp };
