# 82. Match typed direct bodies to general frame layouts

- The typed Go structural emitter now derives capture-slot layouts from the
  general emitter's metadata for eligible left-recursion, Pratt and owned
  repetition scopes. LR bodies use the authoritative shared Node-rule names;
  independent Pratt and repetition scopes use the same build, predicate,
  projection and slot-order rules as the general emitter. The collector also
  visits dead syntax, so omitted captures still have the expected nil slots.
  The runtime clears slots that are not live for a match. Additional required
  slots can widen a frame or pool class, so unchanged allocations in these
  workloads do not establish zero memory cost for every grammar. Node
  generation, recognition and TypeScript output are unchanged. Eligible
  recovery bodies use the separate structural route in
  [record 83](083-inline-typed-recovery-bodies.md); unmatched recovery layouts
  remain on general dispatch.
- The generation-only `disableTypedFrameLayouts` control retains the previous
  discovery and fallback route for same-generator comparisons. It selects no
  runtime branch. Layouts that still cannot be represented by the structural
  emitter remain on the general typed path.
- The eight-case corpus checks direct/reference and conversion parity in both
  position units, including dead captures before and after live captures,
  infallible alternatives, element predicates, projections and lookahead.
  Additional checks compare collected slot metadata with the actual general
  emitter for value-building and recognition paths, including twins and dead
  syntax. Workload tests cover parser reuse, pool reuse and returned AST
  ownership.
- Five alternating 300 ms pairs used Go 1.27.1 on darwin/arm64, Apple M3 Max,
  16 CPUs. Matching ratios are medians of direct/general time ratios, with
  paired min–max ranges:

  | Workload | n | Unit | Median ratio | Paired range |
  |:--|--:|:--|--:|--:|
  | Left recursion | 1 | CodePoints | 0.965× | 0.942–1.005× |
  | Left recursion | 1 | Bytes | 0.969× | 0.927–1.017× |
  | Left recursion | 128 | CodePoints | 0.907× | 0.888–0.937× |
  | Left recursion | 128 | Bytes | 0.934× | 0.877–0.984× |
  | Pratt | 1 | CodePoints | 0.939× | 0.921–1.007× |
  | Pratt | 1 | Bytes | 0.970× | 0.953–1.042× |
  | Pratt | 128 | CodePoints | 0.904× | 0.871–0.937× |
  | Pratt | 128 | Bytes | 0.945× | 0.929–0.996× |

  Every paired ratio for the larger inputs is below parity; all small-input
  ranges cross parity. Allocation counts are unchanged at 4/11 for LR and
  5/12 for Pratt at n=1/128, respectively; cumulative bytes are about
  11.9/41.6 KB and 12.4/45.1 KB.
- The separate generation benchmark includes generation, formatting and
  runtime embedding; grammar parsing is outside its timed loop. Ratios are
  medians of five paired direct/general ratios, with paired min–max ranges:

  | Fixture | Median ratio | Paired range |
  |:--|--:|--:|
  | Left recursion | 0.989× | 0.964–1.061× |
  | Pratt | 0.972× | 0.942–1.019× |
  | Calculator | 1.007× | 0.988–1.105× |
  | TypeScript | 0.983× | 0.960–1.007× |

  All paired ranges cross parity. Generated source is 109,223 versus 111,790
  bytes for LR and 112,143 versus 113,845 bytes for Pratt; calculator and
  TypeScript source sizes are unchanged. Layout collection adds generation-only
  work, including slot-map allocations; these measurements do not establish
  a generation-time benefit or cost. The matching results are synthetic and do not establish a broad
  parser speedup.

Generate the direct and same-generator reference workload fixtures and check
parity:

```sh
export PEGO_TYPED_FRAME_LAYOUT_DIR=/tmp/pego-typed-frame-layouts
go test ./internal/engine -run '^TestGeneratedTypedFrameLayoutWorkloads$' -count=1
```

Build the direct/general fixtures into separate binaries:

```sh
for workload in lr pratt; do
  for variant in direct general; do
    fixture="$PEGO_TYPED_FRAME_LAYOUT_DIR/$workload/$variant"
    (cd "$fixture" && go test -c -o "$PEGO_TYPED_FRAME_LAYOUT_DIR/$workload-$variant.test" .)
  done
done
```

Alternate five paired runs of `BenchmarkTypedLR` and `BenchmarkTypedPratt`
with `-test.run='^$' -test.benchtime=300ms -test.benchmem`, reversing
direct/general order between pairs. The matching loop in
[record 81](081-scoped-cuts-in-unfinished-typed-go-bodies.md) can be reused
with `PEGO_TYPED_FRAMED_CUT_DIR="$PEGO_TYPED_FRAME_LAYOUT_DIR"`. The inputs
use the same `lrInput(n)` and `prattInput(n)` workloads: n=1/128 means two/129
LR groups and one/128 Pratt postfix groups after the base atom.

To compare generation separately, keep `PEGO_REFERENCE_FRAME_LAYOUTS` fixed
before each timed run:

```sh
PEGO_REFERENCE_FRAME_LAYOUTS=0 go test ./internal/engine -run '^$' \
  -bench '^BenchmarkTypedFrameLayoutGeneration$' -benchtime=300ms -benchmem
PEGO_REFERENCE_FRAME_LAYOUTS=1 go test ./internal/engine -run '^$' \
  -bench '^BenchmarkTypedFrameLayoutGeneration$' -benchtime=300ms -benchmem
```

The generation fixtures parse their grammar before the timer and then measure
`Generate`; alternate the reference setting across five pairs. Source sizes
are reported as a benchmark metric, not as a generated-parser runtime measure.
