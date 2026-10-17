// RabbitMQ consumer and publisher (AMQP 0-9-1).
//
//   screening.requested        Hiring → us        (shared, plain durable queue)
//   screening.completed        us → Hiring        (shared, plain durable queue)
//   screening.requested.retry  ours: each message expires after RETRY_DELAY_MS
//                              and is dead-lettered back onto screening.requested
//   screening.requested.dlq    ours: messages that failed for good, kept for
//                              a person to inspect
//
// The two shared queues are declared with no special arguments, so the Hiring
// Service can declare them too without a PRECONDITION_FAILED clash.

const amqp = require("amqplib");
const crypto = require("crypto");
const config = require("./config");
const log = require("./log");
const { processScreeningRequest, markFailed, PermanentError } = require("./pipeline");

const q = config.amqp;
let channel = null;
let stopping = false;

const isReady = () => channel !== null;

async function declareTopology(ch) {
  await ch.assertQueue(q.requestedQueue, { durable: true });
  await ch.assertQueue(q.completedQueue, { durable: true });
  await ch.assertQueue(q.deadLetterQueue, { durable: true });
  await ch.assertQueue(q.retryQueue, {
    durable: true,
    // The delay is set per message (expiration), not on the queue, so changing
    // RETRY_DELAY_MS never clashes with the queue's existing declaration.
    arguments: {
      "x-dead-letter-exchange": "", // default exchange ...
      "x-dead-letter-routing-key": q.requestedQueue, // ... back to the main queue
    },
  });
}

function publishCompleted(ch, completed) {
  ch.sendToQueue(q.completedQueue, Buffer.from(JSON.stringify(completed)), {
    persistent: true,
    contentType: "application/json",
    headers: { "x-correlation-id": completed.correlation_id || "" },
  });
}

// Ack the request only once everything it produced is safely in the broker
// (publisher confirms). If that fails, leave it unacked: RabbitMQ redelivers it
// and the (batch_id, resume_id) key makes the second run a replay.
async function settle(ch, raw, fields) {
  try {
    await ch.waitForConfirms();
    ch.ack(raw);
    return true;
  } catch (err) {
    log.error("could not hand the outcome to RabbitMQ; the request will be redelivered", { ...fields, error: err.message });
    try {
      ch.nack(raw, false, true);
    } catch {
      // channel already closed: the broker requeues unacked messages itself
    }
    return false;
  }
}

async function handle(ch, raw) {
  const started = Date.now();
  const headers = raw.properties.headers || {};
  const attempt = Number(headers["x-retry-count"] || 0);
  let msg = null;

  try {
    try {
      msg = JSON.parse(raw.content.toString());
    } catch {
      throw new PermanentError("Message body is not valid JSON");
    }
    msg.correlation_id = msg.correlation_id || headers["x-correlation-id"] || crypto.randomUUID();

    const { completed, replayed } = await processScreeningRequest(msg);
    publishCompleted(ch, completed);
    if (!(await settle(ch, raw, { correlation_id: msg.correlation_id, batch_id: msg.batch_id, resume_id: msg.resume_id }))) return;
    log.info(replayed ? "screening replayed (already done)" : "screening done", {
      correlation_id: msg.correlation_id,
      batch_id: msg.batch_id,
      resume_id: msg.resume_id,
      candidate_id: completed.candidate_id,
      status: completed.status,
      score: completed.score,
      attempt,
      duration_ms: Date.now() - started,
    });
  } catch (err) {
    const permanent = err instanceof PermanentError;
    const fields = {
      correlation_id: msg?.correlation_id,
      batch_id: msg?.batch_id,
      resume_id: msg?.resume_id,
      attempt,
      error: err.message,
    };

    if (!permanent && attempt < q.maxRetries) {
      // Transient (AI Service or storage down): try again after a delay that
      // doubles each time, with ±20% jitter so a batch does not retry in step
      // (ADR-002 rule 7). The broker holds the timer, not this process.
      const delay = backoff(q.retryDelayMs, attempt);
      ch.sendToQueue(q.retryQueue, raw.content, {
        persistent: true,
        expiration: String(delay),
        contentType: "application/json",
        headers: { ...headers, "x-retry-count": attempt + 1 },
      });
      if (!(await settle(ch, raw, fields))) return;
      log.warn("screening failed, will retry", { ...fields, retry_in_ms: delay });
      return;
    }

    // Permanent, or out of retries: dead-letter it and tell Hiring it failed.
    ch.sendToQueue(q.deadLetterQueue, raw.content, {
      persistent: true,
      contentType: raw.properties.contentType || "application/json",
      headers: { ...headers, "x-error": err.message, "x-failed-at": new Date().toISOString() },
    });
    try {
      const completed = await markFailed(msg, err.message);
      if (completed) publishCompleted(ch, completed);
    } catch (e) {
      log.error("could not record the failure", { ...fields, error: e.message });
    }
    if (!(await settle(ch, raw, fields))) return;
    log.error(permanent ? "screening rejected, sent to dead-letter queue" : "screening gave up after retries, sent to dead-letter queue", fields);
  }
}

// 5 s, 10 s, 20 s, ... for RETRY_DELAY_MS=5000, each ±20%.
function backoff(baseMs, attempt) {
  const jitter = 0.8 + Math.random() * 0.4;
  return Math.round(baseMs * 2 ** attempt * jitter);
}

async function start() {
  while (!stopping) {
    try {
      const conn = await amqp.connect(q.url);
      const ch = await conn.createConfirmChannel();
      await declareTopology(ch);
      await ch.prefetch(q.prefetch);
      await ch.consume(q.requestedQueue, (raw) => raw && handle(ch, raw).catch((err) => log.error("unexpected error handling a message", { error: err.message })));
      channel = ch;
      log.info("Consuming from RabbitMQ", { queue: q.requestedQueue, prefetch: q.prefetch });

      // Wait here until the connection drops, then reconnect.
      await new Promise((resolve) => {
        conn.on("close", resolve);
        conn.on("error", (err) => log.warn("RabbitMQ connection error", { error: err.message }));
      });
      channel = null;
      if (!stopping) log.warn("RabbitMQ connection closed, reconnecting");
    } catch (err) {
      channel = null;
      log.warn("RabbitMQ not reachable, retrying in 3s", { error: err.message });
      await new Promise((r) => setTimeout(r, 3000));
    }
  }
}

module.exports = { start, isReady };
