#!/bin/sh
# Install the pinned Linux x86_64 TinyGo and Binaryen toolchains without root.
set -eu

fail() {
  printf '%s\n' "$*" >&2
  exit 1
}

[ "$(uname -s)" = Linux ] || fail 'Playground toolchain bootstrap supports Linux only.'
[ "$(uname -m)" = x86_64 ] || fail 'Playground toolchain bootstrap supports x86_64 only.'

command -v curl >/dev/null 2>&1 || fail 'curl is required to install the Playground toolchains.'
command -v tar >/dev/null 2>&1 || fail 'tar is required to install the Playground toolchains.'
command -v sha256sum >/dev/null 2>&1 || fail 'sha256sum is required to verify the Playground toolchains.'

tinygo_archive=tinygo0.43.0.linux-amd64.tar.gz
tinygo_sha256=ca9fc6d055e3889462dea4450a2e42a92242d88b5c362a7ec39bdb563413d78a
tinygo_url=https://github.com/tinygo-org/tinygo/releases/download/v0.43.0/$tinygo_archive
binaryen_archive=binaryen-version_131-x86_64-linux.tar.gz
binaryen_sha256=b5bf1f0eaf17c63ee588ff7a5954dc8f6ce2c26989051c66f24dfe9ece3e46db
binaryen_url=https://github.com/WebAssembly/binaryen/releases/download/version_131/$binaryen_archive

cache_root=${NETLIFY_CACHE_DIR:-${TMPDIR:-/tmp}/pego-toolchain-cache}
[ ! -L "$cache_root" ] || fail 'Refusing a symlink as the toolchain cache directory.'
mkdir -p "$cache_root"
[ -d "$cache_root" ] || fail 'Toolchain cache path is not a directory.'

work=$(mktemp -d "${TMPDIR:-/tmp}/pego-toolchains.XXXXXX") || fail 'Could not create a temporary toolchain directory.'
cleanup() { rm -rf "$work"; }
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

fetch_verified() {
  archive=$1
  expected=$2
  url=$3
  cached=$cache_root/$archive
  actual=

  if [ -f "$cached" ]; then
    actual=$(sha256sum "$cached")
    actual=${actual%% *}
  fi
  if [ "$actual" != "$expected" ]; then
    rm -f "$cached"
    download=$work/$archive
    curl --fail --location --silent --show-error --retry 3 --output "$download" "$url" || fail "Download failed: $url"
    actual=$(sha256sum "$download")
    actual=${actual%% *}
    [ "$actual" = "$expected" ] || fail "SHA-256 mismatch for $archive"
    mv "$download" "$cached"
  fi
  printf '%s\n' "$cached"
}

tinygo_tar=$(fetch_verified "$tinygo_archive" "$tinygo_sha256" "$tinygo_url")
binaryen_tar=$(fetch_verified "$binaryen_archive" "$binaryen_sha256" "$binaryen_url")

mkdir "$work/extract"
tar -xzf "$tinygo_tar" -C "$work/extract"
tar -xzf "$binaryen_tar" -C "$work/extract"

# Locate executables by their archive-relative role, not by an assumed top-level folder name.
tinygo_bins=$(find "$work/extract" -path '*/bin/tinygo' -print)
tinygo_count=$(printf '%s\n' "$tinygo_bins" | awk 'END { print NR }')
[ "$tinygo_count" -eq 1 ] || fail 'TinyGo archive must contain exactly one bin/tinygo.'
tinygo_bin=$tinygo_bins
tinygo_root=${tinygo_bin%/bin/tinygo}
[ -x "$tinygo_bin" ] || fail 'TinyGo executable is missing.'
[ -f "$tinygo_root/targets/wasm_exec.js" ] || fail 'TinyGo WASM runtime is missing.'

wasm_opt_bins=$(find "$work/extract" -path '*/bin/wasm-opt' -print)
wasm_opt_count=$(printf '%s\n' "$wasm_opt_bins" | awk 'END { print NR }')
[ "$wasm_opt_count" -eq 1 ] || fail 'Binaryen archive must contain exactly one bin/wasm-opt.'
wasm_opt_bin=$wasm_opt_bins
binaryen_root=${wasm_opt_bin%/bin/wasm-opt}
[ -x "$wasm_opt_bin" ] || fail 'Binaryen wasm-opt executable is missing.'

export TINYGOROOT=$tinygo_root
export PATH=${tinygo_bin%/tinygo}:$binaryen_root/bin:$PATH

tinygo_version=$(tinygo version)
case "$tinygo_version" in 'tinygo version 0.43.0 linux/amd64 '*) ;; *) fail "Unexpected TinyGo version or target: $tinygo_version" ;; esac
wasm_opt_version=$(wasm-opt --version)
case "$wasm_opt_version" in 'wasm-opt version 131'*) ;; *) fail "Unexpected Binaryen version: $wasm_opt_version" ;; esac

cd "$(dirname "$0")"
sh build.sh -wasm-mode=dual
