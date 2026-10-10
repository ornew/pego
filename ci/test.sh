#!/bin/sh
# Run the root and every standalone parser module on the selected Go target.
# GOARCH is inherited by generated-parser subprocesses as well as go test.
set -eu
cd "$(dirname "$0")/.."
go vet ./...
go test "$@" ./...
sh parsers/test.sh "$@"
