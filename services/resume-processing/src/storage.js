// Object storage adapter: fetch a resume file from MinIO (S3 API).
// Hiring stores the file; this service only reads it.

const { S3Client, GetObjectCommand } = require("@aws-sdk/client-s3");
const config = require("./config");

const s3 = new S3Client({
  endpoint: config.s3.endpoint,
  region: config.s3.region,
  credentials: { accessKeyId: config.s3.accessKeyId, secretAccessKey: config.s3.secretAccessKey },
  forcePathStyle: true, // MinIO uses path-style URLs
});

class FileNotFoundError extends Error {}

async function fetchResumeFile(bucket, key) {
  try {
    const res = await s3.send(new GetObjectCommand({ Bucket: bucket, Key: key }));
    return Buffer.from(await res.Body.transformToByteArray());
  } catch (err) {
    if (err.name === "NoSuchKey" || err.name === "NoSuchBucket" || err.$metadata?.httpStatusCode === 404) {
      throw new FileNotFoundError(`Resume file not found: ${bucket}/${key}`);
    }
    throw err; // network or storage outage: worth retrying
  }
}

module.exports = { s3, fetchResumeFile, FileNotFoundError };
