#!/bin/sh
# Project theme and special pages use the pinned Nanosite extension APIs.
set -eu
cd "$(dirname "$0")"
revision=$(cat nanosite.rev)
case "$revision" in *[!0-9a-f]*|'') echo 'Invalid Nanosite commit pin.' >&2; exit 1;; esac
[ "${#revision}" -eq 40 ] || { echo 'Use a full Nanosite commit SHA.' >&2; exit 1; }
expected=$(GOWORK=off go list -m -f '{{.Version}}' "github.com/ornew/nanosite@$revision")
replacement=$(GOWORK=off go list -m -f '{{if .Replace}}{{.Replace.Path}}{{end}}' github.com/ornew/nanosite)
[ -z "$replacement" ] || { echo 'Pinned Nanosite dependency must not use a module replacement.' >&2; exit 1; }
actual=$(GOWORK=off go list -m -f '{{.Version}}' github.com/ornew/nanosite)
[ "$actual" = "$expected" ] || {
  echo 'Nanosite dependency differs from site/nanosite.rev; run hack/update-nanosite-skill.sh.' >&2
  exit 1
}
GOWORK=off exec go run . -repo .. -out dist "$@"
