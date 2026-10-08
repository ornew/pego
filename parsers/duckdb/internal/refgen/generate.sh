#!/bin/sh
# Regenerates the reference data of testdata/reference with DuckDB 1.5.6.
#
#   DUCKDB_SRC=/path/to/duckdb PYTHON=/path/to/python internal/refgen/generate.sh
#
# DUCKDB_SRC is a checkout of DuckDB at tag v1.5.6 (only test/sql is read), PYTHON an interpreter that has the
# duckdb module in version 1.5.6 (pip install duckdb==1.5.6). The scratch files go to a temporary directory.
set -e
: "${DUCKDB_SRC:?set DUCKDB_SRC to a checkout of DuckDB v1.5.6}"
PYTHON=${PYTHON:-python3}
here=$(cd "$(dirname "$0")" && pwd)
out=$here/../../testdata/reference
tmp=$(mktemp -d)
mkdir -p "$out"
"$PYTHON" -c 'import duckdb, sys; sys.exit(0 if duckdb.__version__ == "1.5.6" else "duckdb %s, 1.5.6 wanted" % duckdb.__version__)'

"$PYTHON" "$here/extract.py" "$DUCKDB_SRC" "$tmp/blocks.jsonl"
"$PYTHON" "$here/corpus.py" exprs "$tmp/blocks.jsonl" "$tmp/exprs.jsonl"
"$PYTHON" "$here/corpus.py" lexical "$tmp/blocks.jsonl" "$tmp/lexical.jsonl"
"$PYTHON" "$here/corpus.py" keywords "$tmp/blocks.jsonl" "$tmp/keywords.jsonl"
"$PYTHON" "$here/corpus.py" mutants "$tmp/blocks.jsonl" "$tmp/mutants.jsonl" 20000
"$PYTHON" "$here/corpus.py" found "$tmp/blocks.jsonl" "$tmp/found.jsonl"

# the ASTs of SELECT statements are kept for the tests of DuckDB and for the expressions
"$PYTHON" "$here/refgen.py" "$tmp/blocks.jsonl" "$out/tests.jsonl.gz"
"$PYTHON" "$here/refgen.py" "$tmp/exprs.jsonl" "$out/exprs.jsonl.gz"
"$PYTHON" "$here/refgen.py" "$tmp/lexical.jsonl" "$out/lexical.jsonl.gz"
"$PYTHON" "$here/refgen.py" "$tmp/found.jsonl" "$out/found.jsonl.gz"
"$PYTHON" "$here/refgen.py" "$tmp/keywords.jsonl" "$out/keywords.jsonl.gz" --no-shapes
"$PYTHON" "$here/refgen.py" "$tmp/mutants.jsonl" "$out/mutants.jsonl.gz" --no-shapes
rm -rf "$tmp"
ls -l "$out"
