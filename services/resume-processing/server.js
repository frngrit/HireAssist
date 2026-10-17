// Resume Processing Service
//   - consumes screening.requested from RabbitMQ and runs the UC-2 pipeline
//   - serves REST reads of candidate profiles and screening results
// See README.md and MESSAGES.md.

const config = require("./src/config");
const log = require("./src/log");
const db = require("./src/db");
const queue = require("./src/queue");
const { createApp } = require("./src/rest");

async function main() {
  createApp().listen(config.port, () => {
    log.info("Resume Processing Service running", { url: `http://localhost:${config.port}` });
  });

  try {
    await db.connect();
  } catch (err) {
    log.error("MongoDB connection failed", { error: err.message });
    process.exit(1);
  }

  // Runs forever, reconnecting if RabbitMQ goes away.
  queue.start();
}

main();
