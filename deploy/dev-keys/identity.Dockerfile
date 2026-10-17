# The Identity Service for the local Compose stack, with the development key wrapper.
# Built from the repository root: the wrapper lives in deploy/, the code in services/identity.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY services/identity/go.mod services/identity/go.sum ./
RUN go mod download
COPY services/identity/ .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /identity .

FROM alpine:3.20
RUN apk add --no-cache openssl
COPY --from=build /identity /usr/local/bin/identity
COPY deploy/dev-keys/entrypoint.sh /entrypoint.sh
EXPOSE 8081
ENTRYPOINT ["/bin/sh", "/entrypoint.sh", "identity"]
