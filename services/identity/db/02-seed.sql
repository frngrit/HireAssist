-- 02-seed.sql — two synthetic demo members with fixed ids, so every recording starts the same.
-- Runs on the first database start, and again on every `make db-reset`.
--
--   nok@acme.example      admin      password: nok-admin-demo-password
--   somchai@acme.example  recruiter  password: somchai-recruiter-password
--
-- Synthetic data only. Never put a real person's details here.

-- CASCADE because sessions reference members: resetting the demo members also signs them out.
TRUNCATE members CASCADE;

INSERT INTO members (id, email, password_hash, temporary_password, role) VALUES
  ('00000000-0000-4000-8000-000000000001', 'nok@acme.example',
   crypt('nok-admin-demo-password', gen_salt('bf', 12)), false, 'admin'),
  ('00000000-0000-4000-8000-000000000002', 'somchai@acme.example',
   crypt('somchai-recruiter-password', gen_salt('bf', 12)), false, 'recruiter');
