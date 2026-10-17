# The API Gateway for the local Compose stack, with the development key wrapper. The gateway's
# own Dockerfile builds the deployable image; this one adds a shell to read the public key.
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY services/gateway/go.mod services/gateway/go.sum ./
RUN go mod download
COPY services/gateway/ .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /gateway .

FROM alpine:3.20
COPY --from=build /gateway /usr/local/bin/gateway
COPY deploy/dev-keys/entrypoint.sh /entrypoint.sh
EXPOSE 8000
ENTRYPOINT ["/bin/sh", "/entrypoint.sh", "gateway"]
