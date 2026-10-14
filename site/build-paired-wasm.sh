#!/bin/sh
# Explicit comparison build; the normal site build remains Go-only.
set -eu
cd "$(dirname "$0")"
GOWORK=off exec go run ./cmd/paired-wasm -repo .. "$@"
