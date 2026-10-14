# 096. Specialize Generated Go Work-Control Methods

- **Status**: Measured
- **Date**: 2026-10-14

## Problem and Mechanism

Work-control checks add measurable cost to generated Go ordinary parsing and
recognition. A broader ordinary-method specialization reduced part of that
cost, but retained a sizable gap to the pre-work-control baseline. The narrower
experimental variant specializes emitted ordinary methods and clones only the
`newNode`, `newMatch`, `setCapture` and `expect` helpers. It hardens the
generated-method registry and adds a generation-only ablation control. The
internal emitter uses synchronous callbacks and preserves parser/call-local
state rather than introducing escaping aliases or unrelated shadow state. It
does not change the public API or matching semantics.

This is an experimental implementation on a work branch, not a production
change. A broader runtime-helper variant was rejected: it gained about 1–2%
in recognition but made controlled XML parsing about 3% slower. A separate
ordinary-method split experiment was also rejected after three paired 500 ms
samples: JSON/XML recognition was about 2% slower and Outline about 4% slower
against the selected variant; other profiles showed mixed results without a
consistent gain. Correctness tests for that experiment passed, but it was not
adopted.

## Applicability

| Execution profile | Applied / Partial / Not applied / Not applicable | Reason |
|:--|:--|:--|
| Generated Go ordinary Node parse and recognition | Applied | Invocation-local nil-control branch and four helper families |
| Generated Go typed parsing | Applied | Direct typed bodies and Node-to-typed conversion matching |
| Closure and bytecode backends | Not applicable | Different execution paths |
| Generated TypeScript | Not applicable | No corresponding implementation was measured |

## Reproduction

The 131-profile comparison used Go 1.27.1 on an Apple M3 Max
(darwin/arm64), with `GOMAXPROCS=16`, `GOGC=100` and `GOMEMLIMIT=off`.
Three alternating 500 ms samples compared main `77f12f2` with the source-pinned
candidate in this implementation, based on work-control baseline `186e1b0`. The candidate included a source-format fix and regression
tests. Samples were serialized. The opt-in
`TestGeneratedOrdinaryWorkBenchmarkOverlay` writes enabled and disabled
generation overlays for the seven benchmark parser packages; enabled output
must match the checked-in generated source byte-for-byte. The same-source
ablation disables only the specializer while leaving work controls enabled.
The fixed-allocation check uses 100 iterations and five alternating pairs.
Build benchmark binaries before timing and alternate processes under the same
environment; the existing active-control harnesses do not pre-parse before
timing.


```bash
export GOMAXPROCS=16 GOGC=100 GOMEMLIMIT=off
PEGO_ORDINARY_WORK_OVERLAY=/tmp/ordinary-work-overlay \
  go test ./internal/engine -run '^TestGeneratedOrdinaryWorkBenchmarkOverlay$' -count=1

# Build both variants before timing; alternate these binaries in fresh processes.
go test -overlay=/tmp/ordinary-work-overlay/enabled.json \
  -c -o /tmp/ordinary-enabled.test ./bench
go test -overlay=/tmp/ordinary-work-overlay/disabled.json \
  -c -o /tmp/ordinary-disabled.test ./bench
cd bench
/tmp/ordinary-enabled.test -test.run '^$' \
  -test.bench '^Benchmark(Parse|Recognize)$/./^generated' \
  -test.benchmem -test.benchtime 500ms -test.count 1
# Repeat the command with ordinary-disabled.test, alternating at least three pairs.
```

The internal transform accepts only emitted execution methods. The registry
identifies the receiver and method together; missing or duplicate anchors,
receiver replacement/shadowing, and direct control mutation/address escapes
fail generation. Invocation control stays immutable, generated `p`/`c` names
refer to the current parser/context, and built-in action callbacks run
synchronously. This is not a transform for arbitrary Go code. Clone statements
use their own source positions; insertion into original text preserves
controlled statements and comments. Labels are renamed within ordinary clones.
Runtime helpers outside the four families retain their existing nil checks.

## Results

The short focused run used three alternating 500 ms pairs against the stable
default-off work-control candidate. On the sampled JSON and XML profiles,
generated recognition was 20–24% faster and typed parsing was 17–18% faster.
That comparison did not establish parity with the pre-work-control baseline.

The subsequent 131-profile comparison used three paired 500 ms samples against
the exact main baseline. In the 24 generated Go Node parse/recognition profiles,
candidate/main median ratios ranged from 1.039× to 1.332×; in eight typed-parse
profiles, they ranged from 1.082× to 1.234×. Examples include JSON/XML
recognition at 1.083×/1.183×, Minilang recognition at 1.259×, JSON/XML Node
parsing at 1.161–1.184×/1.163–1.172×, and JSON/XML typed parsing at
1.082×/1.191×. These are descriptive paired medians, not a general estimate
for other workloads. The candidate includes the source-format fix and
regression tests. This comparison measures total work-control feature cost;
the separate enabled/disabled comparison isolates this mechanism.

The same-source ablation comparison used three alternating 500 ms pairs over
32 ordinary and eight active profiles. With the specializer enabled, ordinary
generated Node parse/recognition medians were 0.760–0.969× and typed-parse
medians were 0.815–0.936× the disabled variant. Active-control medians were
0.985–1.005×; individual sample ranges remained noisy, including an XML case
with a 9.7% slower sample. These harnesses do not explicitly pre-parse, so
they do not isolate warmed active calls. In a separate fixed-100-iteration
allocation comparison of Minilang `ParseAST`, the time ratio was 0.922× [0.908–0.926], bytes/op
1.000× [0.959–1.071], and allocations/op 1.000× [0.970–1.053]. No persistent
allocation increase was identified in that control.

The selected binaries for JSON, XML and Minilang each exercised Node parsing,
typed AST parsing and recognition. Their generated parser sources match the
corresponding files in the 131-profile candidate manifest. Executable sizes
were:

| Consumer | Main | Same-source ablation | Specialized | Specialized vs main | Specialized vs ablation |
|:--|--:|--:|--:|--:|--:|
| JSON | 3,189,026 B | 3,262,210 B | 3,427,874 B | +7.49% | +5.08% |
| XML | 4,084,338 B | 4,282,530 B | 4,902,866 B | +20.04% | +14.49% |
| Minilang | 3,270,946 B | 3,312,178 B | 3,510,322 B | +7.32% | +5.98% |

The specialized generated sources were 38.75% (JSON), 58.71% (XML) and 37.61%
(Minilang) larger than their same-source ablations. Builds used `go build
-trimpath`; both variants used the same caller exercising all three entry
points. These size results are limited to the listed consumers.

## Decision and Limits

Not adopted. The focused and same-source ablation comparisons show gains on
ordinary generated paths, but the 131-profile comparison still shows
generated-path regressions against main. Prescribed local validation passed; acceptance of the residual feature costs
remains before adoption. The ordinary-method split alternative was measured and rejected.
The broader runtime-helper expansion is also rejected. Do not infer broader
gains or parity outside the measured profiles and three consumer sizes.

## Validation

Local verification used Go 1.27.1, Node.js 24.19.0 and TypeScript 5.9.3.
`go test -timeout=30m ./...` passed all 18 root-module packages; the engine
package took 851.406 seconds. The two skipped engine tests are opt-in artifact
writers, not semantic coverage. The ordinary-work overlay writer was exercised
separately and verified all seven production benchmark parser packages.
`go generate ./parsers ./bench`, every parser module in `parsers/test.sh`, site
tests and the actual site build/link check passed (229 pages, 257 files).
Independent source and evidence review found no unresolved material findings.
These checks describe local output, not remote CI or deployed behavior.
