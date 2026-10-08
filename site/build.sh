#!/bin/sh
# Builds the PEGO web site into site/dist (or the directory given with -out).
#
#   site/build.sh                          # build
#   site/build.sh -serve localhost:8080    # build, then serve the result for a preview
#
# It needs only Go: the generator is the Go module in this directory, and the playground is
# compiled to WebAssembly from ../playground. Netlify runs it (see netlify.toml).
set -eu
cd "$(dirname "$0")"
exec go run . -repo .. -out dist "$@"
