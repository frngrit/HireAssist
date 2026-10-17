// All configuration comes from the environment (ADR-005).
// Defaults match the local docker-compose.yml in this folder.

const path = require("path");

const env = (name, fallback) => process.env[name] ?? fallback;

module.exports = {
  service: "resume-processing",
  port: Number(env("PORT", 3000)),

  mongo: {
    url: env("MONGO_URL", "mongodb://127.0.0.1:27017"),
    db: env("MONGO_DB", "hireassist_resume_processing"),
  },

  amqp: {
    url: env("AMQP_URL", "amqp://guest:guest@127.0.0.1:5672"),
    // Contract with the Hiring Service — see MESSAGES.md
    requestedQueue: env("SCREENING_REQUESTED_QUEUE", "screening.requested"),
    completedQueue: env("SCREENING_COMPLETED_QUEUE", "screening.completed"),
    // Owned by this service only
    retryQueue: env("SCREENING_RETRY_QUEUE", "screening.requested.retry"),
    deadLetterQueue: env("SCREENING_DLQ", "screening.requested.dlq"),
    prefetch: Number(env("PREFETCH", 4)),
    maxRetries: Number(env("MAX_RETRIES", 3)),
    retryDelayMs: Number(env("RETRY_DELAY_MS", 5000)),
  },

  s3: {
    endpoint: env("S3_ENDPOINT", "http://127.0.0.1:9000"), // MinIO
    region: env("S3_REGION", "us-east-1"),
    accessKeyId: env("S3_ACCESS_KEY", "minioadmin"),
    secretAccessKey: env("S3_SECRET_KEY", "minioadmin"),
  },

  ai: {
    address: env("AI_SERVICE_ADDR", "127.0.0.1:50051"),
    // The contract lives once, at the repository root (ADR-005, ADR-007).
    protoPath: env(
      "AI_PROTO_PATH",
      path.resolve(__dirname, "../../../proto/hireassist/ai/v1/ai_service.proto")
    ),
    deadlineMs: Number(env("AI_DEADLINE_MS", 60000)),
  },
};
