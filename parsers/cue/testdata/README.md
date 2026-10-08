# Vendored data

`corpus.tar.gz`, `mutants.txt.gz`, `generated.txt.gz` and `literals.txt.gz` hold the CUE sources of [cue-lang/cue](https://github.com/cue-lang/cue) at
v0.17.1 (commit fc6c0b2ecd3666da92f7053d13fcfbf009b7d7a3, 2026-07-16; Apache License 2.0, Copyright The CUE
Authors, see LICENSE) and the results of its parser, `cuelang.org/go/cue/parser` of the same version, for them.
They are made by `internal/refgen` (a module of its own, since it depends on cuelang.org/go; see the README of the
module) and checked by `corpus_test.go`.

- `corpus.tar.gz`: a gzip-compressed tar archive with 4,061 sources, and for each, `name.ref`, the result of the
  reference parser: `error: ` and its first error for a rejected source, or the canonical syntax tree (positions
  are byte offsets) and the comments of an accepted one. The sources are the 121 `.cue` files of the repository,
  the 3,726 `.cue` files inside its `.txtar` archives (named `archive#file`), the 148 inputs of the tests of
  the reference parser (`parser_test.go#inN`), and the 66 examples of the specification marked as CUE
  (`spec.md#N.kind`).
- `mutants.txt.gz`: for each source, eight mutated inputs (`mutate` in `internal/refgen/mutate.go` and
  `mutate_test.go`: one edit each, at a position chosen by a seeded generator): the hash of the input, whether
  the reference parser accepts it, and a hash of its canonical tree. The inputs themselves are not stored.
- `generated.txt.gz`: 5,000 inputs of each of three families (`strings`, `numbers` and `tokens`; `generate` in
  `internal/refgen/gen.go` and `gen_test.go`) made by a seeded generator, with the same data as the mutants.
- `literals.txt.gz`: the values that `cuelang.org/go/cue/literal` decodes from 4,126 distinct string, integer and
  float literals of the corpus and of 30,000 generated inputs of each family: whether the decoding fails, and the
  value (`literal_test.go`).

The `*.txt` files and their `*.golden` files are inputs of this module's tests and the trees the engine of PEGO
gives for them; see `parsers/parsers_test.go`.
