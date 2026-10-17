# Makefile — the short commands used in development and in the demo. `make` alone lists them.
#
# There is no Go toolchain assumed: every target runs in a container, so the only requirement
# is Docker. Set GO=go to use a local toolchain instead.
GO ?= docker run --rm -v "$(CURDIR)":/src -w /src -v hireassist-gomod:/go/pkg/mod golang:1.25-alpine go

.PHONY: help keys run test vet fmt build

help:
	@echo "make keys   generate a development signing key pair and print what to export"
	@echo "make run    start the gateway on http://localhost:8000"
	@echo "make test   run the tests"
	@echo "make vet    run go vet"
	@echo "make fmt    format every file in place"
	@echo "make build  compile the binary, to check it builds"

# A development key pair, shared with the Identity Service, whose `make keys` writes the same
# file. The private half belongs there, because it signs; the gateway only ever sees the public
# half (ADR-009). An existing pair is kept, so running this on either side does not invalidate
# the tokens the other has already issued. Never reuse these anywhere real.
KEY_FILE ?= /tmp/hireassist-dev-key.pem

keys:
	@test -f $(KEY_FILE) || openssl ecparam -name prime256v1 -genkey -noout -out $(KEY_FILE) 2>/dev/null
	@openssl ec -in $(KEY_FILE) -pubout -out $(KEY_FILE).pub 2>/dev/null
	@echo "private key (the Identity Service signs with this): $(KEY_FILE)"
	@echo
	@echo "export JWT_PUBLIC_KEYS=\"$$(cat $(KEY_FILE).pub)\""

run:
	$(GO) run .

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

build:
	$(GO) build -o /dev/null .
