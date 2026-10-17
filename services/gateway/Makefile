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

# A development key pair. The private half belongs to the Identity Service, which signs; the
# gateway only ever sees the public half (ADR-009). Never reuse these anywhere real.
keys:
	@openssl ecparam -name prime256v1 -genkey -noout -out /tmp/hireassist-dev-key.pem 2>/dev/null
	@openssl ec -in /tmp/hireassist-dev-key.pem -pubout -out /tmp/hireassist-dev-key.pub 2>/dev/null
	@echo "private key (the Identity Service signs with this): /tmp/hireassist-dev-key.pem"
	@echo
	@echo "export JWT_PUBLIC_KEYS=\"$$(cat /tmp/hireassist-dev-key.pub)\""

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
