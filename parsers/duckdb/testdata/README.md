# Test data

## Golden files

`*.txt` are inputs, `*.golden` the trees and errors that the engine returns for them. The tests of
`github.com/ornew/pego/parsers` check them on every backend of the engine, and the tests of this module on the
generated parser. Rewrite them with `go test ./parsers -update`.

## Reference data

`reference/*.jsonl.gz` is what DuckDB 1.5.6 says about each text of seven corpora. The tests compare the parser
with it (see the README of the module for the numbers). One JSON object per line:

| Field | Contents |
|:--|:--|
| `sql` | the text |
| `file`, `gen` | the file of DuckDB's tests where the text was found; for a generated text, the kind of text (`file`) and how it was made (`gen`) |
| `ok` | `true` if `duckdb.extract_statements` accepts the text |
| `kind` | for a rejection, `syntax` if the message is that of the scanner or the grammar, and `semantic` if DuckDB's parser fails after it has parsed the text (the transformer refuses it) |
| `err` | the message of a rejection |
| `stmts` | for an accepted text, the statements as DuckDB splits it: their type (`SELECT`, `CREATE`, ...) and their text (`text` is left out when the text has one statement) |
| `shapes` | for each statement of type `SELECT`, the AST that `json_serialize_sql` returns, with the locations left out and every key whose value is null, empty or false dropped |

| File | Texts | Where they come from |
|:--|--:|:--|
| `tests.jsonl.gz` | 45,912 | the statements and queries of the `test/sql` files of DuckDB v1.5.6 (the first values of the loops expanded; the shapes of the SELECT statements are kept) |
| `exprs.jsonl.gz` | 7,655 | expressions made of every pair of operators (and of an operator and a prefix or a postfix one, BETWEEN, IN, LIKE, ...) and some random chains, so that precedence and associativity show (with shapes) |
| `lexical.jsonl.gz` | 1,219 | numbers, strings, identifiers, operators, comments and white space of the scanner, each in a few positions (with shapes) |
| `keywords.jsonl.gz` | 83,619 | every keyword where a name may stand, in three spellings |
| `mutants.jsonl.gz` | 20,000 | statements of the tests with a token deleted, duplicated, swapped, replaced or inserted, or cut short (seeded: the same on every run) |
| `found.jsonl.gz` | 200 | the texts of `internal/refgen/found.txt`: those on which an earlier version of the parser and DuckDB disagreed, found by mutating the tests with other seeds (1.4 million texts) and by trying the constructs around them |
| `limit_percent.jsonl.gz` | 583 | Source-backed completion controls for LIMIT percentages: operators/prefixes, closed predicates, mixed chains, arithmetic, parentheses, comma forms and OFFSET order (acceptance and statement splitting; no mapper shapes) |

The LIMIT-percent controls use DuckDB 1.5.6's `select_limit_value` production and Bison precedence declarations.
Completed `IN`, `IS`, `ANY` and postfix rules are checked alongside pending lower-precedence operands and high
arithmetic wrapping unresolved prefixes. Their `sql` fields are the reproducible inputs: extract them as JSON
lines with `file` and `gen` preserved, run `refgen.py INPUT OUTPUT --no-shapes` with the exact pinned module,
and compare acceptance/splitting with the fixture. These are durable conformance inputs, not benchmark logs.

`deviations.jsonl` lists the texts of these files on which the parser and DuckDB differ in acceptance, one JSON object
per line with the text (`sql`) and the reason (`why`); the tests tolerate them, and fail if a text listed there no longer
differs.

The data is made by `internal/refgen` and is the same on every run (the random UUID of the type that DuckDB makes
for a PIVOT is replaced by `UUID`). To make it again:

```sh
git clone --depth 1 --branch v1.5.6 https://github.com/duckdb/duckdb /tmp/duckdb   # only test/sql is read
python3 -m venv /tmp/venv && /tmp/venv/bin/pip install duckdb==1.5.6
DUCKDB_SRC=/tmp/duckdb PYTHON=/tmp/venv/bin/python parsers/duckdb/internal/refgen/generate.sh
```

To compare the parser with DuckDB on more texts than the vendored ones, for example more mutations of the tests (another
seed), make them with the same tools and name the file in `REFERENCE_FILE`:

```sh
cd parsers/duckdb
python3 internal/refgen/extract.py /tmp/duckdb /tmp/blocks.jsonl
python3 internal/refgen/corpus.py mutants /tmp/blocks.jsonl /tmp/more.jsonl 300000 424242   # count and seed
python3 internal/refgen/refgen.py /tmp/more.jsonl /tmp/more-ref.jsonl --no-shapes
REFERENCE_FILE=/tmp/more-ref.jsonl go test -run TestReferenceFile -v
```

The same for random expressions, with their ASTs (precedence and associativity; 250,000 of them, with other seeds, gave
identical trees in 114,000 SELECT statements and the same acceptance):

```sh
python3 internal/refgen/corpus.py exprs /tmp/blocks.jsonl /tmp/exprs.jsonl 250000 12        # count and seed
python3 internal/refgen/refgen.py /tmp/exprs.jsonl /tmp/exprs-ref.jsonl
REFERENCE_FILE=/tmp/exprs-ref.jsonl go test -run TestReferenceFile -v
```

A text on which they differ and that is worth keeping goes into `internal/refgen/found.txt` once it is fixed (or into
`deviations.jsonl`, with the reason, if it is not).

The texts of `tests.jsonl.gz` and the keyword lists in `internal/refgen/keywords` come from DuckDB, which is
distributed under the MIT license (`LICENSE-DuckDB`). The grammar of DuckDB descends from the one of PostgreSQL
(through libpg_query), which has a license of its own (BSD); this module does not contain any of their code.
