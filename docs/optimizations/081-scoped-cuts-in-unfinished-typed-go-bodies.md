# 81. Scoped cuts in unfinished typed Go bodies

- Eligible cut-bearing left-recursion leader bodies and Pratt operand,
  operator and trivia matchers now use the unfinished structural emitter. The
  runtime retains capture frames, left-recursion growth, Pratt selection and
  action finalization. Node eligibility, ordinary typed cuts and TypeScript
  output are unchanged.
- The emitter initializes each body's lexical cut state from the incoming
  `p.cut`, publishes the resulting state on both returns, and keeps nested
  choice, optional and repetition scopes separate. A cut executed in
  lookahead is discarded when lookahead resets. Repetition rollback applies
  `cutExpr` and `cutFail` after restoring its full mark and dropping children.
- The generation-only `disableTypedFramedCuts` reference control emits the
  same runtime with per-expression bodies. It adds no generated-parser option
  or matching-time selector. `#recover` and structurally unsupported bodies
  remain on general dispatch.
- The 14-case parity corpus checks ordinary cut expressions and actions in
  Pratt operands, left-recursion captures and repetitions, Pratt longest-match
  and trivia behavior, prefix-part cuts, rollback, nested scopes and
  expectation isolation. Existing cut-free LR/Pratt parity suites also pass.
  Workload tests compare trees, values and errors across direct/reference
  routes, parser reuse, manual parser reuse, returned AST ownership, both
  position units and Node conversion.
- Five alternating 300 ms pairs used Go 1.27.1 on darwin/arm64, Apple M3 Max,
  16 CPUs. Ratios are medians of direct/reference time ratios with paired
  min–max ranges:

  | Workload | n | Unit | Median ratio | Paired range |
  |:--|--:|:--|--:|--:|
  | Left recursion | 1 | CodePoints | 0.958× | 0.941–0.989× |
  | Left recursion | 1 | Bytes | 0.991× | 0.950–1.013× |
  | Left recursion | 128 | CodePoints | 0.911× | 0.898–0.951× |
  | Left recursion | 128 | Bytes | 0.943× | 0.935–0.981× |
  | Pratt | 1 | CodePoints | 0.993× | 0.987–1.051× |
  | Pratt | 1 | Bytes | 1.004× | 1.003–1.040× |
  | Pratt | 128 | CodePoints | 0.889× | 0.878–0.914× |
  | Pratt | 128 | Bytes | 0.932× | 0.919–0.977× |

  Large captures improve in both grammars and units. The small Pratt Bytes
  case is 0.4% slower at the median (paired range 0.3–4.0%). The small
  LR CodePoints case improves by about 4%; the other small cases overlap
  parity. Allocations remain 11/12 for the large LR/Pratt
  inputs, with about 38.6/44.5 KB per operation; small inputs remain at 4/5
  allocations and about 11.9/12.3 KB. These synthetic comparisons do not
  establish a broad parser speedup; generation and compilation are excluded.

Generate same-revision direct and reference fixtures and check parity:

```sh
export PEGO_TYPED_FRAMED_CUT_DIR=/tmp/pego-typed-framed-cuts
go test ./internal/engine -run '^TestGeneratedTypedFramedCutWorkloads$' -count=1
```

Build each fixture as a separate benchmark binary:

```sh
for workload in lr pratt; do
  for variant in direct general; do
    fixture="$PEGO_TYPED_FRAMED_CUT_DIR/$workload/$variant"
    (cd "$fixture" && go test -c -o "$PEGO_TYPED_FRAMED_CUT_DIR/$workload-$variant.test" .)
  done
done
```

Alternate run order across five pairs, benchmarking the corresponding
workload in each fixture:

```sh
for pair in 1 2 3 4 5; do
  if [ "$((pair % 2))" -eq 1 ]; then
    first=general; second=direct
  else
    first=direct; second=general
  fi
  for workload in lr pratt; do
    for variant in "$first" "$second"; do
      "$PEGO_TYPED_FRAMED_CUT_DIR/$workload-$variant.test" -test.run='^$' \
        -test.bench='^BenchmarkTyped(LR|Pratt)$' -test.benchtime=300ms -test.benchmem
    done
  done
done
```

The LR workload uses `lrInput(n) = "a,b,c," +
strings.Repeat(";d,e,f,", n)`; `n=1` has two groups and `n=128` has 129.
The Pratt workload uses `prattInput(n) = "a" +
strings.Repeat(" [b,c,d,]", n)`; `n` counts postfix groups after the base
atom. The measurements compare the current structural route with its
same-generator per-expression reference, not with a historical parent.
