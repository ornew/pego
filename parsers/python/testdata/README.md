# Test Data

The golden files (`*.txt` with `*.golden`) are written by the engine (`go test ./parsers -update` from the root of
the repository) and show the shape of the trees; the module's `TestGolden` checks the generated parser against them.

The rest of the data is about [CPython](https://github.com/python/cpython) 3.14.0 (tag `v3.14.0`, commit
`ebf955df7a89ed0c7968f79faec1de49f61ed7cb`, PSF License). The text it is about is not stored here: the corpus `cpython`
(`go run ./hack/corpora fetch cpython`) holds `Lib/test`, and each line says where its text is in it.

- `cpython-snippets.jsonl.gz`: the 61,540 distinct snippets of Python code (at most 5,000 characters) in the string
  constants and doctest examples of CPython's `Lib/test`, each with whether `ast.parse` accepts it and the first 16
  hexadecimal digits of the SHA-256 of `ast.dump(tree)` without and with positions (`include_attributes=True`). One
  JSON object per line, in the order of the snippets:

  | Field | Contents |
  |:--|:--|
  | `f`, `o`, `n` | the file under `Lib/test`, and the offset and size in bytes of a range of it |
  | `p` | where the snippet is not that range as it is (a string constant that spells escapes, a doctest example with the prompts and the indentation of its lines removed): the pieces that make it, `[offset, size]` for a copy of a part of the range and a string for text of its own. 7,513 snippets have pieces, 54,027 are the range |
  | `h` | the first 8 hexadecimal digits of the SHA-256 of the snippet, which the tests check against the text they make |
  | `ok`, `error`, `hash`, `phash` | the result of `ast.parse`: whether it accepts, the class of the error, the hashes of the dumps |

- `cpython-files.jsonl.gz`: the same for 13 whole files of `Lib/test` that exercise the grammar (`test_grammar`,
  `test_patma`, `test_fstring`, `test_tstring`, `test_syntax`, `test_type_params`, `test_type_aliases`,
  `test_unparse`, `test_positional_only_arg`, `test_keywordonlyarg`, `test_with`, `test_ast/snippets` and
  `test_future_stmt/test_future`), with `path` (under `Lib/test`) and `h` instead of `f`, `o`, `n`, `p`.

The text in `p` (12.7 KB in all, at most 347 characters in one snippet) is the transformation that turns the range
into the snippet, such as the prompts and the indentation that a doctest example drops or the spelling of an escape,
not upstream code; it is kept as data of this repository.

The inputs of the benchmarks, `Lib/_pydecimal.py` and `Lib/typing.py`, are not here: the corpus `cpython` (see
`internal/corpora/manifest.json` in the root of the repository) holds them, and `go run ./hack/corpora fetch cpython`
puts them in the cache.

`internal/refgen/generate.sh` regenerates the two `jsonl.gz` files (see the README of the module).
