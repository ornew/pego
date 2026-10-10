# 78. Local cuts in typed direct rules

- The typed Go generator now inlines eligible ordinary rules whose bodies
  contain local cuts. The direct emitter represents cut scopes locally while
  keeping capture, variable-environment, recovered-error and input rollback
  aligned with the general typed path. `#recover`, Pratt expressions and
  cut-bearing left-recursion leaders remain on general dispatch;
  [record 79](079-inline-eligible-typed-left-recursion-bodies.md) adds eligible
  cut-free leader bodies. Node generation,
  recognition and TypeScript output are unchanged.
- The cut control is a generation-time internal option used for differential
  tests and benchmarks. It adds no generated parser option or matching-time
  branch. Both routes are emitted by the same latest generator; the general
  route is not a build from the historical parent, which did not have this
  direct-cut implementation.
- `TestGeneratedTypedDirectCuts` compares generated route selection and
  typed results/errors with the general route across 12 grammar cases and
  both position units. Cases cover nested choices, optional/repeated scopes,
  projections, lookahead, cut-bearing callees, memoized captures, Unicode,
  and rollback of outer captures and variables. The wider typed corpus also
  checks Node conversion; pooled-result tests run with the direct-cut route
  enabled and disabled. The workload fixture checks successful and malformed
  inputs, both units, parser reuse and unchanged returned values.
- Focused benchmark: Go 1.27.1, darwin/arm64, Apple M3 Max, 16 CPUs; five
  alternating 300 ms pairs, with each generated binary built before timing.
  Ratios are medians of the direct/general time ratios across pairs;
  every paired range is below 1:

  | Input | Unit | Median ratio | Paired range |
  |:--|:--|--:|--:|
  | 2 rows | CodePoints | 0.825× | 0.791–0.879× |
  | 2 rows | Bytes | 0.880× | 0.810–0.910× |
  | 512 rows | CodePoints | 0.562× | 0.523–0.580× |
  | 512 rows | Bytes | 0.712× | 0.693–0.768× |

  Allocation counts remain 5 per operation for two rows and 15 for 512 rows.
  Cumulative bytes are about 12.1 KB and 86.4 KB per operation, with small
  variation between the routes. The timed operation is `ParseAST`; generation
  and compilation are excluded. No cold-compiler claim is made.
  These synthetic workloads isolate the cut extension. All 17 tracked
  generated parsers are byte-identical after regeneration; the existing
  full-suite snapshot does not measure these synthetic cut workloads.

To regenerate both latest-generator controls and check that they return the
same values and errors:

```sh
export PEGO_TYPED_CUT_DIR=/tmp/pego-typed-cuts
go test ./internal/engine -run '^TestGeneratedTypedCutWorkload$' -count=1
```

Build each fixture into its own benchmark binary:

```sh
for variant in direct general; do
  (cd "$PEGO_TYPED_CUT_DIR/$variant" && \
    go test -c -o "$PEGO_TYPED_CUT_DIR/$variant.test" .)
done
```

Run five alternating pairs; alternate which binary runs first on each pair:

```sh
for pair in 1 2 3 4 5; do
  if [ "$((pair % 2))" -eq 1 ]; then
    first=general; second=direct
  else
    first=direct; second=general
  fi
  "$PEGO_TYPED_CUT_DIR/$first.test" -test.run='^$' \
    -test.bench='^BenchmarkTypedCuts$' -test.benchtime=300ms -test.benchmem
  "$PEGO_TYPED_CUT_DIR/$second.test" -test.run='^$' \
    -test.bench='^BenchmarkTypedCuts$' -test.benchtime=300ms -test.benchmem
done
```

The benchmark grammar has two cut-bearing alternatives for each typed row:

```pego
type Row struct { Key Match, Value Match }
type Doc struct { Rows []Row }
def main: Doc = rows:row+ $$ -> new Doc{Rows: $rows}
def row: Row = ("let" " " -- key:@((?a-z)+) ":" value:@((?0-9)+) ";"
              / "var" " " -- key:@((?a-z)+) ":" value:@((?0-9)+) ";") -> new Row{Key: $key, Value: $value}
```

`cutInput(1)` contains two rows; `cutInput(256)` contains 512. The generated
fixture also checks malformed rows, non-ASCII input, trailing input and reuse
of a previously returned AST. These timings measure the specialized emitter
against its same-revision general-route control, not against a historical
parent revision.
