#!/bin/sh
# Runs the tests of every parser module (each subdirectory with a go.mod) and the checks of the main
# module on them (generated code up to date, golden files on every backend). Arguments are passed to
# go test, for example parsers/test.sh -short.
set -e
cd "$(dirname "$0")"
go test "$@" .
for mod in */go.mod; do
	dir=$(dirname "$mod")
	echo "== $dir"
	(cd "$dir" && go vet ./... && go test "$@" ./...)
done
