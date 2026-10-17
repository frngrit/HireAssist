# Makefile — the short commands used in development and in the demo. `make` alone lists them.

DB_CONTAINER := hireassist-identity-db
PSQL         := docker exec -i $(DB_CONTAINER) psql -U identity -d identity
# The local Docker database from docker-compose.yml. Development only; never a real password.
DATABASE_URL ?= postgres://identity:identity@localhost:5433/identity?sslmode=disable
# The development signing key (ADR-009). `make keys` writes this pair; the gateway is given the
# public half. Never a key used anywhere real.
KEY_FILE     ?= /tmp/hireassist-dev-key.pem
JWT_KEY_ID   ?= dev-1
# password_hash is cut to its first 20 characters so the table fits the screen: enough to show
# it is a bcrypt hash ($2a$12$ and the start of the salt), never the password itself.
SHOW_SQL     := SELECT left(id::text, 8) || '…' AS id, email, role, left(password_hash, 20) || '…' AS password_hash, temporary_password AS temp_pw, to_char(created_at, 'HH24:MI:SS') AS created, to_char(updated_at, 'HH24:MI:SS') AS updated, coalesce(to_char(removed_at, 'HH24:MI:SS'), '') AS removed FROM members ORDER BY created_at, email

.PHONY: help keys db-up db-down db-reset db-schema db-show db-watch db-shell run demo test

help:
	@echo "make db-up      start PostgreSQL in Docker (the first start creates the table and 2 demo members)"
	@echo "make keys       generate the development signing key pair in $(KEY_FILE)"
	@echo "make run        start the Identity Service on http://localhost:8081"
	@echo "make db-reset   put the 2 demo members back — run before each recording"
	@echo "make db-schema  apply db/01-schema.sql to a database created before sessions existed"
	@echo "make test       run the tests"
	@echo "make db-show    print the members table"
	@echo "make db-watch   re-print the table every 2 seconds — keep it open beside Postman (Ctrl+C stops)"
	@echo "make db-shell   open psql inside the container (\\q to leave)"
	@echo "make demo       walk through every call with curl, showing the table after each change"
	@echo "make db-down    stop PostgreSQL; data is kept ('docker compose down -v' wipes it)"

db-up:
	docker compose up -d --wait

db-down:
	docker compose down

# The private half stays here; the gateway and every other verifier get only the public half.
keys:
	@test -f $(KEY_FILE) || openssl ecparam -name prime256v1 -genkey -noout -out $(KEY_FILE) 2>/dev/null
	@openssl ec -in $(KEY_FILE) -pubout -out $(KEY_FILE).pub 2>/dev/null
	@echo "signing key: $(KEY_FILE)   (key id $(JWT_KEY_ID))"
	@echo
	@echo "give the gateway the public half:"
	@echo "export JWT_PUBLIC_KEYS=\"$$(cat $(KEY_FILE).pub)\""

db-schema:
	@$(PSQL) -q < db/01-schema.sql && echo "schema applied"

db-reset:
	@$(PSQL) -q < db/02-seed.sql && echo "members reset to the 2 demo members"

db-show:
	@$(PSQL) -c "$(SHOW_SQL)"

db-watch:
	@trap 'exit 0' INT; while true; do clear; echo "members — refreshed $$(date '+%H:%M:%S')   (Ctrl+C to stop)"; $(PSQL) -c "$(SHOW_SQL)"; sleep 2; done

db-shell:
	docker exec -it $(DB_CONTAINER) psql -U identity -d identity

# JWT_PRIVATE_KEY comes from the key file unless it is already set in the environment. The key
# is passed in, never read from a path inside the repository, so nothing can commit it.
run: keys
	DATABASE_URL='$(DATABASE_URL)' JWT_KEY_ID='$(JWT_KEY_ID)' JWT_PRIVATE_KEY="$$(cat $(KEY_FILE))" go run .

test:
	go test ./...

demo:
	sh demo.sh
