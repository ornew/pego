# 84. Complete typed Go-local capture layouts

This extends the generator at `268634e`. Measurements compare the candidate
with its own generation-only reference control, rather than an older revision.

The typed Go direct emitter retains the complete ordered capture-name layout
for ordinary rule bodies and owned repetition elements. Ordinary rules seed
their names from shared Node-rule metadata; owned repetition and
projected-repetition scopes use the general emitter's collector at the same
build mode, including dead syntax, predicate visibility, projection and slot
ordering. This lets structural emission omit unreachable captures while
preserving the zero/nil slot values of the equivalent frame.

Names are known eagerly, but Go locals are emitted only when matching,
mark/reset, or term/attached-value access needs them. A predicate that refers
to a known capture omitted from dead syntax can still read a nil local. The
locals do not allocate runtime capture frames. This change applies to ordinary
typed direct rules and owned repetition scopes; framed LR, Pratt and recovery
routes are unchanged. The Node and TypeScript generators are unchanged.

The generation-only `disableTypedLocalLayouts` control restores the prior
structural-discovery route for same-generator comparisons. It adds no
generated-parser option or matching-time selector.

`TestGeneratedTypedLocalLayouts` checks 12 typed grammars in both position
units against the same-generator control and Node conversion. Cases include
dead captures before and after live captures, predicates, element scopes,
projection, repetition reset, cuts, lookahead and diagnostics.
`TestGeneratedTypedLocalLayoutWorkloads` checks ordinary, predicate-bearing
element and cut-bearing workloads on accepted and rejected UTF-8 input at
sizes 1 and 128. It compares manual and pooled parser reuse and verifies
returned AST and diagnostic stability, rollback and cleared invocation state.

Five alternating 300 ms pairs on Go 1.27.1, darwin/arm64, Apple M3 Max
(16 CPUs) compared prebuilt parsers from the same generator. Each ratio is the
median of the five paired direct/general ratios; ranges show the minimum and
maximum paired ratios. At 128 repetitions, all twelve accepted/rejected,
CodePoints/Bytes ranges were below 1:

| Workload | Accepted CP / Bytes | Rejected CP / Bytes |
| --- | --- | --- |
| Ordinary | .711 [.668–.759] / .736 [.721–.766] | .716 [.692–.738] / .765 [.726–.774] |
| Element predicate | .751 [.710–.790] / .775 [.754–.805] | .764 [.733–.797] / .797 [.759–.818] |
| Cuts | .797 [.761–.840] / .812 [.796–.870] | .758 [.728–.789] / .829 [.776–.838] |

At size 1, medians ranged from .848 to .967 and every allocation count matched
its control; the rejected ordinary Bytes range crossed parity (.885–1.003).
Allocations are unchanged at both sizes: ordinary and element cases allocate
7 times on acceptance and 10 on rejection; cuts allocate 8/10 at size 1 and
135/137 at size 128. Allocated bytes are similar, with pooled-memory variation.
These synthetic cases establish a focused benefit, not a full-parser speedup.

Generation was measured separately with five alternating 300 ms pairs, with
grammar parsing outside the timed loop. Source, memory and allocation columns
are direct/general medians:

| Fixture | Ratio [paired range] | Source bytes | B/op | allocs/op |
| --- | --- | ---: | ---: | ---: |
| Ordinary | .922 [.856–.950] | 103,037 / 104,839 | 3,332,333 / 3,399,192 | 54,508 / 56,097 |
| Element | .903 [.850–.962] | 108,497 / 111,284 | 3,579,891 / 3,680,454 | 59,134 / 61,486 |
| Cuts | .949 [.854–.964] | 104,392 / 106,119 | 3,359,241 / 3,415,277 | 55,558 / 57,206 |

The calculator control was .983 [.935–1.031]; the TypeScript-grammar control
was .970 [.967–.993]. Both emit identical output with the control enabled or
disabled. These controls do not attribute a parser-time effect or establish a
general generation-time benefit. All 17 tracked generated parser outputs
remain unchanged. Layout collection adds generator work and omitted nil
slots may require additional Go locals; the measurements do not prove absence
of costs for every grammar.

Generate same-generator direct and reference fixtures and check value and
error parity:

```sh
export PEGO_TYPED_LOCAL_LAYOUT_DIR=/tmp/pego-typed-local-layouts
go test ./internal/engine -run '^TestGeneratedTypedLocalLayoutWorkloads$' -count=1
```

Build each workload and route as a separate benchmark binary:

```sh
for workload in ordinary element cuts; do
  for variant in direct general; do
    fixture="$PEGO_TYPED_LOCAL_LAYOUT_DIR/$workload/$variant"
    (cd "$fixture" && go test -c -o "$PEGO_TYPED_LOCAL_LAYOUT_DIR/$workload-$variant.test" .)
  done
done
```

Alternate the direct/general order across five paired runs:

```sh
for pair in 1 2 3 4 5; do
  if [ "$((pair % 2))" -eq 1 ]; then
    first=general; second=direct
  else
    first=direct; second=general
  fi
  for workload in ordinary element cuts; do
    for variant in "$first" "$second"; do
      "$PEGO_TYPED_LOCAL_LAYOUT_DIR/$workload-$variant.test" -test.run='^$' \
        -test.bench='^BenchmarkTypedLocalLayout$' -test.benchtime=300ms -test.benchmem
    done
  done
done
```

For generation, compile the engine test binary once. Run it from
`internal/engine`, where its fixture paths resolve, alternating
`PEGO_REFERENCE_LOCAL_LAYOUTS=0` and `1` across five pairs:

```sh
go test -c -o /tmp/pego-typed-local-layout-engine.test ./internal/engine
(cd internal/engine && for pair in 1 2 3 4 5; do
  if [ "$((pair % 2))" -eq 1 ]; then first=1; second=0; else first=0; second=1; fi
  for reference in "$first" "$second"; do
    PEGO_REFERENCE_LOCAL_LAYOUTS=$reference /tmp/pego-typed-local-layout-engine.test \
      -test.run='^$' -test.bench='^BenchmarkTypedLocalLayoutGeneration$' \
      -test.benchtime=300ms -test.benchmem
  done
done)
```
