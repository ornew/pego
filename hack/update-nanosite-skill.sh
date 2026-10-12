#!/bin/sh
# Synchronize the skill and optional extension dependency to one Nanosite commit.
# Usage: sh hack/update-nanosite-skill.sh [FULL_COMMIT_SHA]
set -eu
repo_dir=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd -P)
pin_file="$repo_dir/site/nanosite.rev"
revision=${1:-$(cat "$pin_file")}
case "$revision" in *[!0-9a-f]*|'') echo 'Use a full lowercase 40-character commit SHA.' >&2; exit 1;; esac
[ "${#revision}" -eq 40 ] || { echo 'Use a full 40-character commit SHA.' >&2; exit 1; }
skill_dir="$repo_dir/.agents/skills/nanosite"
for destination in "$skill_dir" "$pin_file" "$repo_dir/site/go.mod" "$repo_dir/site/go.sum"; do
  [ ! -L "$destination" ] || { echo "Refusing symlink: $destination" >&2; exit 1; }
done
stage=$(mktemp -d "${TMPDIR:-/tmp}/nanosite-sync.XXXXXX")
mutating=false
complete=false
restore() {
  destination=$1
  name=$2
  rm -rf -- "$destination"
  if [ -e "$stage/old-$name" ]; then cp -R "$stage/old-$name" "$destination"; fi
}
cleanup() {
  code=$?
  trap - EXIT HUP INT TERM
  if [ "$mutating" = true ] && [ "$complete" = false ]; then
    restore "$skill_dir" skill
    restore "$pin_file" pin
    if [ -f "$stage/site.mod" ]; then
      restore "$repo_dir/site/go.mod" mod
      restore "$repo_dir/site/go.sum" sum
    fi
  fi
  rm -rf -- "$stage"
  exit "$code"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
# Validate existing contents through the selected command, before changing them.
[ ! -e "$skill_dir" ] || cp -R "$skill_dir" "$stage/nanosite"
GOWORK=off go run "github.com/ornew/nanosite@$revision" skill -out "$stage/nanosite"
if [ -f "$repo_dir/site/go.mod" ]; then
  version=$(GOWORK=off go -C "$repo_dir/site" list -m -f '{{.Version}}' "github.com/ornew/nanosite@$revision")
  cp "$repo_dir/site/go.mod" "$stage/site.mod"
  [ ! -f "$repo_dir/site/go.sum" ] || cp "$repo_dir/site/go.sum" "$stage/site.sum"
  GOWORK=off go -C "$repo_dir/site" mod edit -modfile="$stage/site.mod" "-require=github.com/ornew/nanosite@$version"
  GOWORK=off go -C "$repo_dir/site" mod edit -modfile="$stage/site.mod" -dropreplace=github.com/ornew/nanosite
  GOWORK=off go -C "$repo_dir/site" mod tidy -modfile="$stage/site.mod"
fi
[ ! -e "$skill_dir" ] || cp -R "$skill_dir" "$stage/old-skill"
[ ! -e "$pin_file" ] || cp "$pin_file" "$stage/old-pin"
[ ! -e "$repo_dir/site/go.mod" ] || cp "$repo_dir/site/go.mod" "$stage/old-mod"
[ ! -e "$repo_dir/site/go.sum" ] || cp "$repo_dir/site/go.sum" "$stage/old-sum"
mkdir -p "$(dirname "$skill_dir")"
mutating=true
rm -rf -- "$skill_dir"
cp -R "$stage/nanosite" "$skill_dir"
if [ -f "$stage/site.mod" ]; then
  cp "$stage/site.mod" "$repo_dir/site/go.mod"
  [ ! -f "$stage/site.sum" ] || cp "$stage/site.sum" "$repo_dir/site/go.sum"
fi
printf '%s\n' "$revision" > "$pin_file"
complete=true
printf 'Synchronized Nanosite and its embedded skill at %s\n' "$revision"
