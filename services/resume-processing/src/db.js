// MongoDB models (ADR-003: AI-derived documents live in MongoDB).
// Every document carries candidate_id so retention can delete one person's
// derived data directly. There is no workspace_id: one deployment serves one
// company (ADR-006).

const mongoose = require("mongoose");
const config = require("./config");
const log = require("./log");

async function connect() {
  await mongoose.connect(config.mongo.url, { dbName: config.mongo.db });
  log.info("Connected to MongoDB", { database: config.mongo.db });
}

const isConnected = () => mongoose.connection.readyState === 1;

// ---------- Candidate profile ----------
// Created either by the screening pipeline (from a resume) or by the CRUD
// endpoints used in the earlier demo.
const workExperienceSchema = new mongoose.Schema(
  { title: String, organisation: String, start: String, end: String, summary: String },
  { _id: false }
);
const educationSchema = new mongoose.Schema(
  { qualification: String, institution: String, completed: String },
  { _id: false }
);

const candidateProfileSchema = new mongoose.Schema({
  _id: { type: String }, // "cand-001", "cand-002", ...
  jobOpeningId: { type: String },
  // Set by the CRUD endpoints only. The AI profile deliberately has no name or
  // contact details (they are excluded from scoring).
  fullName: { type: String },
  email: { type: String },
  skills: { type: [String], default: [] },
  resumeText: { type: String, default: "" },
  // Structured profile produced by AI Service.ExtractProfileFromText
  workHistory: { type: [workExperienceSchema], default: [] },
  education: { type: [educationSchema], default: [] },
  additionalText: { type: String, default: "" },
  // Where the profile came from, when it came from the pipeline
  source: {
    batchId: String,
    resumeId: String,
    bucket: String,
    key: String,
  },
  extraction: { provider: String, model: String, promptVersion: String },
  createdAt: { type: Date, default: Date.now },
});

const CandidateProfile = mongoose.model("CandidateProfile", candidateProfileSchema);

// ---------- Screening result ----------
// One per (batch_id, resume_id): the key that makes redelivery harmless (ADR-002).
const assessmentSchema = new mongoose.Schema(
  { criterionId: String, met: Boolean, score: Number, evidence: String },
  { _id: false }
);

const screeningResultSchema = new mongoose.Schema({
  _id: { type: String }, // `${batchId}:${resumeId}`
  batchId: { type: String, required: true, index: true },
  resumeId: { type: String, required: true },
  jobOpeningId: { type: String },
  candidateId: { type: String, index: true },
  status: { type: String, enum: ["processing", "scored", "unparsable", "failed"], required: true },
  score: Number,
  meetsMustHaves: Boolean,
  unmetMustHaveIds: { type: [String], default: [] },
  assessments: { type: [assessmentSchema], default: [] },
  justification: String,
  scoring: { provider: String, model: String, promptVersion: String },
  // The criteria snapshot this resume was judged against
  criteria: { type: Array, default: [] },
  error: String,
  attempts: { type: Number, default: 0 },
  createdAt: { type: Date, default: Date.now },
  updatedAt: { type: Date, default: Date.now },
});

const ScreeningResult = mongoose.model("ScreeningResult", screeningResultSchema);

// ---------- Candidate id sequence ----------
// Several resumes are processed at once, and none may get the same id.
const counterSchema = new mongoose.Schema({ _id: String, seq: { type: Number, default: 0 } });
const Counter = mongoose.model("Counter", counterSchema);

// Atomic on MongoDB. Each id is also claimed with a plain insert, whose unique
// _id makes a duplicate impossible even on a store where findOneAndUpdate is
// not atomic, and the first-ever upsert race (E11000) just tries again.
const IssuedId = mongoose.model("IssuedCandidateId", new mongoose.Schema({ _id: String }));
const isDuplicateKey = (err) => err?.code === 11000;

async function nextCandidateId() {
  for (let attempt = 0; attempt < 20; attempt++) {
    try {
      const counter = await Counter.findOneAndUpdate(
        { _id: "candidate" },
        { $inc: { seq: 1 } },
        { upsert: true, new: true }
      );
      const id = "cand-" + String(counter.seq).padStart(3, "0");
      await IssuedId.create({ _id: id });
      return id;
    } catch (err) {
      if (!isDuplicateKey(err)) throw err;
    }
  }
  throw new Error("Could not allocate a candidate id");
}

module.exports = { connect, isConnected, CandidateProfile, ScreeningResult, nextCandidateId };
