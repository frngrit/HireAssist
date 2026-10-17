# Short commands for the Hiring Service.

# The local stack from the root docker-compose.yml. Development only; never real credentials.
INTEGRATION := \
	HIRING_TEST_DATABASE_URL='postgres://hireassist:hireassist@localhost:5432/hiring_test?sslmode=disable' \
	HIRING_TEST_MONGO_URL='mongodb://localhost:27017' \
	HIRING_TEST_RABBITMQ_URL='amqp://hireassist:hireassist@localhost:5672/' \
	HIRING_TEST_S3_ENDPOINT='localhost:9000' HIRING_TEST_S3_ACCESS_KEY=hireassist HIRING_TEST_S3_SECRET_KEY=hireassist-local

.PHONY: help test test-all fake-screener

help:
	@echo "make test           unit, contract and in-memory tests"
	@echo "make test-all       the same plus the integration tests (needs: docker compose up -d postgres mongo rabbitmq minio)"
	@echo "make fake-screener  stand in for Resume Processing against the local broker (development only)"

test:
	go test -race ./...

test-all:
	$(INTEGRATION) go test -race -count=1 ./...

fake-screener:
	go run ./cmd/fake-screener
