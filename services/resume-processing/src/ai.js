// gRPC client for the AI Service (ADR-007). Generated at runtime from the
// shared contract in proto/hireassist/ai/v1/ai_service.proto.

const grpc = require("@grpc/grpc-js");
const protoLoader = require("@grpc/proto-loader");
const config = require("./config");

const definition = protoLoader.loadSync(config.ai.protoPath, {
  keepCase: true,
  longs: Number,
  enums: String,
  defaults: true,
  oneofs: true,
});
const { AiService } = grpc.loadPackageDefinition(definition).hireassist.ai.v1;

const client = new AiService(config.ai.address, grpc.credentials.createInsecure());

// gRPC status codes that mean "try again later" rather than "this input is bad"
const TRANSIENT = new Set([
  grpc.status.UNAVAILABLE,
  grpc.status.DEADLINE_EXCEEDED,
  grpc.status.RESOURCE_EXHAUSTED,
  grpc.status.ABORTED,
  grpc.status.INTERNAL,
  grpc.status.UNKNOWN,
]);

const isTransient = (err) => err && typeof err.code === "number" && TRANSIENT.has(err.code);

function call(method, request) {
  return new Promise((resolve, reject) => {
    const deadline = new Date(Date.now() + config.ai.deadlineMs);
    client[method](request, { deadline }, (err, response) => (err ? reject(err) : resolve(response)));
  });
}

const LANGUAGE = { th: "LANGUAGE_THAI", en: "LANGUAGE_ENGLISH" };
const toLanguage = (code) => LANGUAGE[(code || "").toLowerCase()] || "LANGUAGE_UNSPECIFIED";

module.exports = {
  isTransient,
  toLanguage,
  extractProfileFromText: (req) => call("ExtractProfileFromText", req),
  scoreAgainstCriteria: (req) => call("ScoreAgainstCriteria", req),
};
