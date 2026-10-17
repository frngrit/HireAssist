#!/bin/sh
# Loads the development signing key pair from the shared volume into the environment, then
# starts the service. ADR-009 has the Identity Service hold the private half and the gateway
# only the public half; both services read keys from environment variables, which Compose
# cannot fill from a file, hence this wrapper.
#
#   identity: creates the pair on first start if the volume is empty
#   gateway:  waits for the public half
set -eu
KEYS=/keys
case "${1:?usage: entrypoint.sh identity|gateway}" in
  identity)
    if [ ! -f "$KEYS/dev-key.pem" ]; then
      openssl ecparam -name prime256v1 -genkey -noout -out "$KEYS/dev-key.pem"
      openssl ec -in "$KEYS/dev-key.pem" -pubout -out "$KEYS/dev-key.pem.pub" 2>/dev/null
    fi
    JWT_PRIVATE_KEY="$(cat "$KEYS/dev-key.pem")"
    export JWT_PRIVATE_KEY
    exec /usr/local/bin/identity
    ;;
  gateway)
    until [ -f "$KEYS/dev-key.pem.pub" ]; do sleep 1; done
    JWT_PUBLIC_KEYS="$(cat "$KEYS/dev-key.pem.pub")"
    export JWT_PUBLIC_KEYS
    exec /usr/local/bin/gateway
    ;;
esac
