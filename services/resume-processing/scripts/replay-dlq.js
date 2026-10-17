// Move every message in the dead-letter queue back onto screening.requested,
// with its retry count reset. Use it after fixing whatever made them fail
// (for example: the AI Service was down).
//
// Usage: npm run replay-dlq

const amqp = require("amqplib");
const config = require("../src/config");

async function main() {
  const conn = await amqp.connect(config.amqp.url);
  const ch = await conn.createChannel();
  const { messageCount } = await ch.assertQueue(config.amqp.deadLetterQueue, { durable: true });
  await ch.assertQueue(config.amqp.requestedQueue, { durable: true });

  // Move only what is there now: a message that fails again while we are
  // working lands back in the dead-letter queue and must not be picked up twice.
  let moved = 0;
  while (moved < messageCount) {
    const msg = await ch.get(config.amqp.deadLetterQueue, { noAck: false });
    if (!msg) break;
    const { "x-retry-count": _r, "x-error": error, "x-failed-at": _f, ...headers } = msg.properties.headers || {};
    ch.sendToQueue(config.amqp.requestedQueue, msg.content, {
      persistent: true,
      contentType: "application/json",
      headers,
    });
    ch.ack(msg);
    const body = JSON.parse(msg.content.toString());
    console.log(`replayed ${body.batch_id}/${body.resume_id}   (was: ${error})`);
    moved++;
  }
  console.log(moved ? `\n${moved} message(s) moved back to ${config.amqp.requestedQueue}` : "dead-letter queue is empty");
  await ch.close();
  await conn.close();
}

main().catch((err) => {
  console.error(err.message);
  process.exit(1);
});
