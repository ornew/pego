# 80. Inline eligible typed Pratt line matchers

- The generated typed Go runtime inlines eligible unfinished operand, operator
  and skip matchers on Pratt lines. Each line owns its `tprattLine.scope`, so
  it does not need a shared Node scope cache. The Pratt runtime retains
  precedence selection, longest-match behavior, frames and action finalization.
- A generation-time reference control emits per-expression methods for those
  matchers while keeping the same Pratt runtime. It adds no parser-time option
  or branch. Node generation, recognition and TypeScript output are unchanged.
- This cut-free route leaves cut-bearing matchers to the framed body emitter
  in [change 81](081-scoped-cuts-in-unfinished-typed-go-bodies.md).
  `#recover`, action captures absent from the actual general frame and
  layouts the collector cannot represent remain on the general typed path.
  [Record 82](082-match-typed-direct-frame-layouts.md) recovers cases where the earlier structural walk omitted or
  reordered dead captures. Eligible callees may still use direct bodies.
- The 14-case typed corpus compares the direct and same-generator reference
  routes across associativity, longest-match ties, captures, rollback,
  lookahead, nullable parts, cut/recovery fallback, unreachable captures,
  mixed routes and error isolation. It also checks Node output is byte-identical
  and runs conversion parity in both position units. Workload fixtures check
  accepted and rejected inputs, parser reuse and unchanged returned values.
- Focused benchmark: Go 1.27.1, darwin/arm64, Apple M3 Max, 16 CPUs; five
  alternating 300 ms pairs of prebuilt direct/general binaries. Ratios are
  medians of direct/general time ratios across pairs, with paired min–max ranges:

  | Workload | n | Unit | Median ratio | Paired range |
  |:--|--:|:--|--:|--:|
  | Calculator | 1 | CodePoints | 0.953× | 0.913–0.998× |
  | Calculator | 1 | Bytes | 0.972× | 0.920–0.994× |
  | Calculator | 128 | CodePoints | 0.939× | 0.884–0.970× |
  | Calculator | 128 | Bytes | 0.995× | 0.954–1.032× |
  | Actions and captures | 1 | CodePoints | 1.017× | 0.971–1.041× |
  | Actions and captures | 1 | Bytes | 0.987× | 0.944–1.040× |
  | Actions and captures | 128 | CodePoints | 0.891× | 0.881–0.932× |
  | Actions and captures | 128 | Bytes | 0.940× | 0.891–0.984× |

  Both large action/capture cases improve, with allocation counts unchanged
  at 12 per operation and cumulative bytes about 44.5 KB. The small action
  cases show no stable benefit (5 allocations and about 12.3 KB). Calculator
  allocation counts also stay unchanged (4/14) with about 11.7/61.8 KB;
  large-input Bytes shows no established gain. Node and recognition control
  ranges cross parity. These synthetic measurements do not establish a broad
  parser speedup; generation and compilation are excluded.

To generate the same-generator routes and check that they return identical
values and errors:

```sh
export PEGO_TYPED_PRATT_DIR=/tmp/pego-typed-pratt
go test ./internal/engine -run '^TestGeneratedTypedPratt(Workload|ActionWorkload)$' -count=1
```

Build each fixture from the `direct` and `general` folders into separate
benchmark binaries:

```sh
for workload in calculator actions; do
  fixture=$PEGO_TYPED_PRATT_DIR
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
    fixture=$PEGO_TYPED_PRATT_DIR
    if [ "$workload" = actions ]; then fixture="$fixture/actions"; fi
    for variant in "$first" "$second"; do
      "$fixture/$variant.test" -test.run='^$' \
        -test.bench='^BenchmarkTypedPratt$' -test.benchtime=300ms -test.benchmem
    done
  done
done
```

The calculator uses `lrInput(n) = "1" + strings.Repeat("+2*3", n)`.
The action/capture fixture uses `prattInput(n) = "a" +
strings.Repeat(" [b,c,d,]", n)`; `n=128` produces 128 postfix groups
after the base atom.
