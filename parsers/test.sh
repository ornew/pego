#!/bin/sh
# Fetches the third-party test corpora, then runs the tests of every parser module (each subdirectory with a go.mod)
# and the checks of the main module on them (generated code up to date, golden files on every backend). Arguments are
# passed to go test, for example parsers/test.sh -short. The corpora are fetched into $PEGO_CORPORA (default: the
# user cache directory); a failed fetch stops the run, so that no test is skipped for a missing corpus.
set -e
cd "$(dirname "$0")/.."
go run ./hack/corpora fetch || { echo "parsers/test.sh: fetching the corpora failed" >&2; exit 1; }
cd parsers
go test "$@" .
for mod in */go.mod; do
	dir=$(dirname "$mod")
	echo "== $dir"
	(cd "$dir" && go vet ./... && go test "$@" ./...)
done
