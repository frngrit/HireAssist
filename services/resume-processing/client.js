// Demo client: calls each REST endpoint in turn to walk through CRUD.
// It pauses after every step so you can check MongoDB Atlas in between.
// Run `npm start` first, then `npm run client` in a second terminal.

const readline = require("readline");

const BASE = "http://localhost:3000";

const rl = readline.createInterface({ input: process.stdin });
const lines = rl[Symbol.asyncIterator]();
async function pause() {
  process.stdout.write("\n>>> Press Enter for the next step...");
  await lines.next();
}

async function call(method, path, body) {
  const res = await fetch(BASE + path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  });
  const data = await res.json();
  console.log(`${method} ${path}  →  HTTP ${res.status}`);
  console.log(data);
  return data;
}

async function main() {
  console.log("\n=== 1. Create (POST) ===");
  const created = await call("POST", "/candidates", {
    job_opening_id: "job-002",
    full_name: "สมหญิง รักงาน",
    email: "somying@example.com",
    skills: ["Python", "SQL"],
    resume_text: "3 ปีประสบการณ์ data analyst",
  });
  await pause();

  console.log("\n=== 2. Read one (GET) ===");
  await call("GET", `/candidates/${created.id}`);
  await pause();

  console.log("\n=== 3. Read list (GET, filtered by job opening) ===");
  await call("GET", "/candidates?jobOpeningId=job-002");
  await pause();

  console.log("\n=== 4. Update (PUT) ===");
  await call("PUT", `/candidates/${created.id}`, {
    full_name: "สมหญิง รักงาน (Updated)",
    skills: [...created.skills, "REST"],
  });
  await pause();

  console.log("\n=== 5. Delete (DELETE) ===");
  await call("DELETE", `/candidates/${created.id}`);
  await pause();

  console.log("\n=== 6. Read again (expect 404: the profile is gone) ===");
  await call("GET", `/candidates/${created.id}`);

  rl.close();
}

main();
