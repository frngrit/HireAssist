// Structured JSON logs, one object per line (ADR-005).
const config = require("./config");

function log(level, msg, fields = {}) {
  console.log(JSON.stringify({ time: new Date().toISOString(), level, service: config.service, msg, ...fields }));
}

module.exports = {
  info: (msg, fields) => log("info", msg, fields),
  warn: (msg, fields) => log("warn", msg, fields),
  error: (msg, fields) => log("error", msg, fields),
};
