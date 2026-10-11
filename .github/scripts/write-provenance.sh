#!/bin/sh
# Save the runtime and reference-corpus versions alongside each CI test log artifact.
set -eu

out=${1:?usage: write-provenance.sh output-file}
{
	if command -v go >/dev/null 2>&1; then go version; else echo 'go: unavailable'; fi
	if command -v node >/dev/null 2>&1; then echo "node: $(node --version)"; else echo 'node: unavailable'; fi
	python=${PEGO_PYTHON:-python}
	if command -v "$python" >/dev/null 2>&1; then echo "python: $("$python" --version 2>&1)"; else echo 'python: unavailable'; fi
	if command -v tsc >/dev/null 2>&1; then echo "tsc: $(tsc --version)"; else echo 'tsc: unavailable'; fi
	if [ -n "${PEGO_TYPESCRIPT:-}" ] && [ -d "$PEGO_TYPESCRIPT" ]; then
		version=$(node -e 'process.stdout.write(require(process.argv[1]+"/package.json").version)' "$PEGO_TYPESCRIPT")
		echo "TypeScript package: $version ($PEGO_TYPESCRIPT)"
	else
		echo 'TypeScript package: not used in this job'
	fi
	if [ -n "${PEGO_TYPESCRIPT_REF:-}" ]; then
		echo "TypeScript reference checkout: $(git -C "$PEGO_TYPESCRIPT_REF" rev-parse HEAD)"
	else
		echo 'TypeScript reference checkout: not used in this job'
	fi
	if [ -n "${PEGO_TYPESCRIPT_TESTS:-}" ]; then echo "TypeScript corpus: $PEGO_TYPESCRIPT_TESTS"; else echo 'TypeScript corpus: not used in this job'; fi
	if [ -n "${PEGO_CPYTHON_SRC:-}" ]; then echo "CPython source corpus: $PEGO_CPYTHON_SRC"; else echo 'CPython Lib/test source corpus: not supplied'; fi
	if [ -n "${REFERENCE_FILE:-}" ]; then echo "DuckDB supplemental reference file: $REFERENCE_FILE"; else echo 'DuckDB supplemental REFERENCE_FILE: not supplied'; fi
} >"$out"
cat "$out"
