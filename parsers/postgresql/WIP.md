# parsers/postgresql: work in progress

This branch (`feat/parser-postgresql`) holds an unfinished parser of PostgreSQL 18's SQL, saved when the work was
paused on 2026-10-09. It is based on main at 8707f18; rebase it onto main before continuing (main has since gained
engine fixes, other parsers and `parsers/generate.go` lines: keep every line of generate.go, sorted).

## State

- Committed earlier (3f34ff1): the module skeleton, the generator of the reference data (`internal/refgen/gen.py`:
  splits PostgreSQL's regression scripts as psql does and records what libpg_query, through pglast 8.5 / PostgreSQL
  18.6, accepts and returns), the keyword tables, and the first part of the grammar: scanner rules, expressions as
  Pratt expressions, types, SELECT and its clauses, INSERT, UPDATE, DELETE and MERGE. On the regression scripts of
  those statements the trees equal the reference's for every statement that both accept.
- Saved in this WIP commit, unverified (the tests did not pass at the moment of saving, and the last edit was cut
  off: "the name shadows the stdlib `types`. Rename."):
  - `postgresql.pego`, `parser.go` (generated, 8 MB): the grammar as concatenated from the parts below at that
    moment; `parsers/generate.go` has its line.
  - Go code: `decode.go` (literals and identifiers), `split.go` (statements of a script), `rawjson.go`,
    `rawjson_hooks.go`, `rawschema.go` (conversion of the typed AST to the JSON of the raw parse tree, for the
    comparison with the reference), with tests (`decode_test.go`, `split_test.go`, `regress_test.go`,
    `example_test.go`, `bench_test.go`) and `testdata/regress.jsonl.gz` (reference data).
  - `internal/refgen/rawschema.py`: prints the node types of the raw tree with their fields.
  - `pgdev/` at the repository root: a development command (not for merging) that parses a corpus with a grammar
    through the engine and writes the results as JSON lines.
- **`internal/wip/scratch/`**: the working directory of the agent, which lived in a temporary directory. The grammar
  was being written in parts (`main/parts/*.pego`, concatenated in name order: 00-types, 10-lexical, 20-names,
  30-expr, 40-select, 50-dml, 51/52-shared, 60-tables, 61-objects, 62-utility, 63-xmljson, 90-main) and the
  statements of gram.y were split into regions given to sub-agents, each in `ag-<group>/` with its own parts, test
  cases (`cases/*.sql`) and comparison tools. `AGENTS.md` is the brief they followed (conventions of the grammar, the
  tools, the deliverables); `gram.condensed` is gram.y without its C code (from `condense.py`); `pgrun.sh`,
  `pgcases.sh`, `cmp.py` and `p.py` run a grammar on the corpus and compare with the reference. Files over 1 MB,
  generated Go code, binaries and corpus outputs were not saved: regenerate them with the scripts.

## To continue

1. Rebase onto main, regenerate (`go generate ./parsers`) and get `parsers/test.sh` green.
2. Merge the regions of `internal/wip/scratch/ag-*/parts` into the grammar (the parts of `main/parts` are the core;
   each `ag-*` directory replaced a stub `stmt_x` of its region), and compare the whole regression corpus with the
   reference (see `AGENTS.md` for the commands; the reference is pglast in a Python venv:
   `uv venv ref-sql && VIRTUAL_ENV=ref-sql uv pip install pglast duckdb`; PostgreSQL's sources at tag REL_18_0:
   `git clone --depth 1 --branch REL_18_0 https://github.com/postgres/postgres`).
3. Finish the module as the other parsers are (README with measured conformance, package doc, examples, benchmarks),
   then remove `pgdev/`, `internal/wip/` and this file before merging into main.
