# Test data

The golden files (`*.txt` with `*.golden`) are written by the engine (`go test ./parsers -update` from the root of
the repository) and show the shape of the trees; the module's `TestGolden` checks the generated parser against them.

The rest of the data comes from [CPython](https://github.com/python/cpython) 3.14.0 (tag `v3.14.0`, commit
`ebf955df7a89ed0c7968f79faec1de49f61ed7cb`), which is under the [PSF License](CPYTHON-LICENSE):

- `cpython-snippets.jsonl.gz`: the 61,540 distinct snippets of Python code (at most 5,000 characters) in the string
  constants and doctest examples of CPython's `Lib/test`, each with whether `ast.parse` accepts it and the first 16
  hexadecimal digits of the SHA-256 of `ast.dump(tree)` without and with positions (`include_attributes=True`).
  The snippets are source text, copied from the test suite.
- `cpython-files.jsonl.gz`: the same for 13 whole files of `Lib/test` that exercise the grammar (`test_grammar`,
  `test_patma`, `test_fstring`, `test_tstring`, `test_syntax`, `test_type_params`, `test_type_aliases`,
  `test_unparse`, `test_positional_only_arg`, `test_keywordonlyarg`, `test_with`, `test_ast/snippets` and
  `test_future_stmt/test_future`).
- `bench/_pydecimal.py` and `bench/typing.py`: files of CPython's `Lib` (unchanged), the inputs of the benchmarks.

`internal/refgen/generate.sh` regenerates the two `jsonl.gz` files (see the README of the module).
