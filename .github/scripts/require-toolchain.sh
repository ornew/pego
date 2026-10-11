#!/bin/sh
# Fail fast when a CI job is not running the exact runtime its test harness expects.
set -eu

require_version() {
	name=$1
	actual=$2
	expected=$3
	if [ "$actual" != "$expected" ]; then
		echo "$name version mismatch: expected $expected, got $actual" >&2
		exit 1
	fi
}

if [ "${REQUIRE_NODE:-}" = "1" ]; then
	require_version node "$(node --version)" "v22.18.0"
fi

if [ "${REQUIRE_TSC:-}" = "1" ]; then
	require_version tsc "$(tsc --version)" "Version 5.9.3"
fi

if [ "${REQUIRE_PYTHON:-}" = "1" ]; then
	python=${PEGO_PYTHON:-python3}
	require_version Python "$("$python" --version 2>&1)" "Python 3.14.0"
	"$python" -c 'import sys; assert sys.version_info[:3] == (3, 14, 0), sys.version'
fi

if [ "${REQUIRE_TYPESCRIPT_CORPUS:-}" = "1" ]; then
	: "${PEGO_TYPESCRIPT:?PEGO_TYPESCRIPT must name the TypeScript 5.9.3 package}"
	: "${PEGO_TYPESCRIPT_TESTS:?PEGO_TYPESCRIPT_TESTS must name the v5.9.3 tests/cases corpus}"
	node -e 'const p=require(process.argv[1]+"/package.json"); if (p.version !== "5.9.3") throw Error(`expected TypeScript 5.9.3, got ${p.version}`)' "$PEGO_TYPESCRIPT"
	test -d "$PEGO_TYPESCRIPT_TESTS"
fi
