#!/bin/sh
# Regenerates the vendored reference data of the tests (testdata/cpython-*.jsonl.gz) with CPython
# 3.14.0:
#
#   PEGO_PYTHON=python3.14 internal/refgen/generate.sh /path/to/cpython-checkout
#
# The checkout is of the tag v3.14.0 (commit ebf955df7a89ed0c7968f79faec1de49f61ed7cb). Run it from
# the root of the module.
set -e
py=${PEGO_PYTHON:-python3.14}
src=${1:?usage: generate.sh CPYTHON_CHECKOUT}
test -f "$src/Lib/test/test_grammar.py" || { echo "no Lib/test in $src" >&2; exit 1; }
cd "$(dirname "$0")/../.."
# The snippets: the string constants and doctest examples of Lib/test, with whether ast.parse
# accepts them and the hashes of the dumps of their trees.
"$py" -I -c "$(cat internal/refgen/refgen.py)" -b -x "$src/Lib/test" | gzip -9n > testdata/cpython-snippets.jsonl.gz
# Whole files of Lib/test that test the grammar.
t="$src/Lib/test"
for f in test_grammar test_patma test_fstring test_tstring test_syntax test_type_params test_type_aliases \
	test_unparse test_positional_only_arg test_keywordonlyarg test_with test_ast/snippets test_future_stmt/test_future; do
	echo "$t/$f.py"
done | "$py" -I -c "$(cat internal/refgen/refgen.py)" -b | gzip -9n > testdata/cpython-files.jsonl.gz
ls -l testdata/cpython-*.jsonl.gz
