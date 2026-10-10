# 79. Inline eligible typed left-recursion bodies

- The generated typed Go runtime can compile eligible left-recursion leader
  expression bodies with the structural direct emitter. The runtime keeps
  ownership of the leader's seed growth, invocation, memoization, action and
  finish behavior. Node generation and recognition are unchanged.
- The generation-time reference control emits the same eligible bodies as
  per-expression methods while retaining the same left-recursion runtime.
  This isolates the body-inlining group without adding a parser-time option or
  branch. Ordinary typed direct rules and ordinary local-cut rules keep their
  existing routes.
- Eligibility is limited to bodies the structural emitter can lower without
  changing the leader's runtime frame layout. Leader bodies containing cuts,
  `#recover`, action captures outside the structural frame layout and
  frame-layout mismatches remain on the general typed path for this cut-free
  route. Eligible cut-bearing LR/Pratt bodies use the framed emitter in
  [change 81](081-scoped-cuts-in-unfinished-typed-go-bodies.md); cut-free Pratt
  lines have their own matcher route in [change 80](080-inline-eligible-typed-pratt-lines.md).
  Eligible callees may still be direct.
- The parity corpus compares the enabled and same-generator reference routes
  for direct and indirect recursion, hidden and nullable seeds, failed or
  non-improving growth, projected and element captures, environment rollback,
  memoized lookahead and unreachable captures. It also checks generated Node
  output is byte-identical and runs the typed/conversion corpus in both
  position units. Workload fixtures use the left-recursive calculator and an
  action/capture-heavy grammar; they check accepted and rejected inputs, both
  units, parser reuse and unchanged returned values.
- Focused benchmark: Go 1.27.1, darwin/arm64, Apple M3 Max; five alternating
  300 ms pairs of prebuilt direct/general binaries. Ratios are medians of the
  direct/general time ratios across pairs, with paired min–max ranges:

  | Workload | n | Unit | Median ratio | Paired range |
  |:--|--:|:--|--:|--:|
  | Calculator | 1 | CodePoints | 0.994× | 0.949–1.074× |
  | Calculator | 1 | Bytes | 0.977× | 0.932–1.101× |
  | Calculator | 128 | CodePoints | 0.994× | 0.976–1.092× |
  | Calculator | 128 | Bytes | 1.005× | 0.952–1.040× |
  | Actions and captures | 1 | CodePoints | 0.976× | 0.963–1.002× |
  | Actions and captures | 1 | Bytes | 0.969× | 0.914–0.991× |
  | Actions and captures | 128 | CodePoints | 0.893× | 0.886–0.939× |
  | Actions and captures | 128 | Bytes | 0.943× | 0.892–0.965× |

  Calculator ratios show no stable benefit: all paired ranges cross parity.
  The larger action/capture workload improves in both units; its 11
  allocations per operation remain unchanged and cumulative bytes are about
  38.6 KB. Small action/capture input uses 4 allocations per operation with
  cumulative bytes about 11.9 KB. These synthetic workloads compare two
  routes from the same generator revision; they do not represent a full-suite
  speedup. Node generation and recognition are outside this optimization.

To generate the same-generator routes and check that they return identical
values and errors:

```sh
export PEGO_TYPED_LR_DIR=/tmp/pego-typed-lr
go test ./internal/engine -run '^TestGeneratedTypedLR(Workload|ActionWorkload)$' -count=1
```

Build the `direct` and `general` fixtures into separate benchmark binaries.
The calculator fixtures are under `$PEGO_TYPED_LR_DIR/{direct,general}`;
the action/capture fixtures are under
`$PEGO_TYPED_LR_DIR/actions/{direct,general}`:

```sh
for workload in calculator actions; do
  fixture=$PEGO_TYPED_LR_DIR
  if [ "$workload" = actions ]; then fixture="$fixture/actions"; fi
  for variant in direct general; do
    (cd "$fixture/$variant" && go test -c -o "$fixture/$variant.test" .)
  done
done
```

Alternate run order over five paired samples:

```sh
for pair in 1 2 3 4 5; do
  if [ "$((pair % 2))" -eq 1 ]; then
    first=general; second=direct
  else
    first=direct; second=general
  fi
  for workload in calculator actions; do
    fixture=$PEGO_TYPED_LR_DIR
    if [ "$workload" = actions ]; then fixture="$fixture/actions"; fi
    for variant in "$first" "$second"; do
      "$fixture/$variant.test" -test.run='^$' \
        -test.bench='^BenchmarkTypedLR$' -test.benchtime=300ms -test.benchmem
    done
  done
done
```

The calculator uses `lrInput(n) = "1" + strings.Repeat("+2*3", n)`.
The action/capture fixture uses `lrInput(n) = "a,b,c," +
strings.Repeat(";d,e,f,", n)`; `n=128` therefore contains 129 groups.
Generation and compilation are excluded from the timed operation.
