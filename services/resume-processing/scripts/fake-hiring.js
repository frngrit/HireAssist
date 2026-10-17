// Stand-in for the Hiring Service, so the pipeline can be tested and demoed on
// its own. It does what Hiring will do in UC-2:
//   1. stores resume PDFs in S3 object storage (MinIO or moto locally)
//   2. publishes one screening.requested per resume
//   3. waits for screening.completed and prints the ranked results
//
// Usage:
//   npm run fake-hiring                 # the sample resumes in samples/
//   npm run fake-hiring -- --missing    # also send a resume whose file does not exist
//
// Uses the same environment variables as the service (AMQP_URL, S3_*).

const fs = require("fs");
const path = require("path");
const crypto = require("crypto");
const amqp = require("amqplib");
const { S3Client, PutObjectCommand, CreateBucketCommand } = require("@aws-sdk/client-s3");
const config = require("../src/config");

const BUCKET = process.env.RESUME_BUCKET || "resumes";
const PREFIX = "screening"; // object key prefix in the bucket
const JOB = "job-001";
const TIMEOUT_MS = Number(process.env.FAKE_HIRING_TIMEOUT_MS || 120000);

// Criteria snapshot for "Backend Developer" — the shape of ai_service.proto Criterion
const CRITERIA = [
  { id: "c1", text: "3+ years building backend services", must_have: true, weight: 0, category: "experience" },
  { id: "c2", text: "Strong SQL and relational data modelling", must_have: false, weight: 40, category: "skills" },
  { id: "c3", text: "Experience with message queues such as RabbitMQ", must_have: false, weight: 30, category: "skills" },
  { id: "c4", text: "Cloud or container experience (Docker, Kubernetes)", must_have: false, weight: 30, category: "skills" },
];

const s3 = new S3Client({
  endpoint: config.s3.endpoint,
  region: config.s3.region,
  credentials: { accessKeyId: config.s3.accessKeyId, secretAccessKey: config.s3.secretAccessKey },
  forcePathStyle: true,
});

async function ensureBucket() {
  try {
    await s3.send(new CreateBucketCommand({ Bucket: BUCKET }));
  } catch (err) {
    if (!["BucketAlreadyOwnedByYou", "BucketAlreadyExists"].includes(err.name)) throw err;
  }
}

async function main() {
  const withMissing = process.argv.includes("--missing");
  const batchId = "batch-" + new Date().toISOString().replace(/\D/g, "").slice(0, 14) + "-" + crypto.randomBytes(2).toString("hex");
  const samplesDir = path.join(__dirname, "..", "samples");
  const files = fs.readdirSync(samplesDir).filter((f) => f.endsWith(".pdf")).sort();

  console.log(`\n=== Hiring (fake): batch ${batchId}, job ${JOB}, ${files.length} resume(s) ===\n`);
  await ensureBucket();

  const requests = [];
  for (const [i, file] of files.entries()) {
    const resumeId = `res-${String(i + 1).padStart(3, "0")}`;
    const key = `${PREFIX}/${batchId}/${resumeId}.pdf`;
    await s3.send(
      new PutObjectCommand({
        Bucket: BUCKET,
        Key: key,
        Body: fs.readFileSync(path.join(samplesDir, file)),
        ContentType: "application/pdf",
      })
    );
    console.log(`stored in S3      ${BUCKET}/${key}   (${file})`);
    requests.push({ resumeId, key, file });
  }
  if (withMissing) {
    const resumeId = `res-${String(files.length + 1).padStart(3, "0")}`;
    requests.push({ resumeId, key: `${PREFIX}/${batchId}/does-not-exist.pdf`, file: "(missing file)" });
  }

  const conn = await amqp.connect(config.amqp.url);
  const ch = await conn.createChannel();
  await ch.assertQueue(config.amqp.requestedQueue, { durable: true });
  await ch.assertQueue(config.amqp.completedQueue, { durable: true });

  for (const r of requests) {
    const msg = {
      batch_id: batchId,
      resume_id: r.resumeId,
      job_opening_id: JOB,
      file: { bucket: BUCKET, key: r.key },
      criteria: CRITERIA,
      language: "en",
      correlation_id: crypto.randomUUID(),
    };
    ch.sendToQueue(config.amqp.requestedQueue, Buffer.from(JSON.stringify(msg)), {
      persistent: true,
      contentType: "application/json",
      headers: { "x-correlation-id": msg.correlation_id },
    });
    console.log(`published         screening.requested   ${r.resumeId}`);
  }

  console.log(`\nwaiting for screening.completed ...\n`);
  const results = [];
  await new Promise((resolve) => {
    const timer = setTimeout(resolve, TIMEOUT_MS);
    ch.consume(config.amqp.completedQueue, (raw) => {
      ch.ack(raw);
      const done = JSON.parse(raw.content.toString());
      if (done.batch_id !== batchId) return; // someone else's batch
      results.push(done);
      const file = requests.find((r) => r.resumeId === done.resume_id)?.file;
      console.log(
        `received          screening.completed   ${done.resume_id}  →  ${done.status}` +
          (done.status === "scored" ? `  score=${done.score}  candidate=${done.candidate_id}` : "") +
          (done.error ? `  (${done.error})` : "") +
          `   [${file}]`
      );
      if (results.length === requests.length) {
        clearTimeout(timer);
        resolve();
      }
    });
  });

  const ranked = results.filter((r) => r.status === "scored").sort((a, b) => b.score - a.score);
  console.log(`\n=== Ranked results for ${JOB} ===`);
  ranked.forEach((r, i) =>
    console.log(
      `${i + 1}. ${r.candidate_id}  score ${r.score}  must-haves ${r.meets_must_haves ? "met" : "NOT met " + JSON.stringify(r.unmet_must_have_ids)}`
    )
  );
  const others = results.filter((r) => r.status !== "scored");
  if (others.length) {
    console.log(`\nNeeds attention:`);
    others.forEach((r) => console.log(`- ${r.resume_id}: ${r.status} — ${r.error}`));
  }
  if (results.length < requests.length) {
    console.log(`\n(timed out: ${requests.length - results.length} result(s) still pending — is the AI Service running?)`);
  }
  console.log(`\nDetails: curl "http://localhost:${config.port}/screenings?batchId=${batchId}"\n`);
  await ch.close(); // flushes the last ack before the connection goes away
  await conn.close();
}

main().catch((err) => {
  console.error(err.message);
  process.exit(1);
});
