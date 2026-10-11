# 90. Share identical generated helpers and literal tables

**Status:** Implemented

**Scope:** Generated Go Node and typed parsers, plus generated TypeScript
parsers. Closure and bytecode runtimes are unchanged. No public API or emitted
runtime selector is added.

The generators intern expression helpers only when both the generated method
signature and complete body match. Rule wrappers remain separate, preserving
rule identity, memoization, depth, captures and expectations. Immutable Go
literal tables are shared by decoded text; matcher descriptions and
expectation IDs remain attached to their original matchers.

Go production generation enables both helper and literal sharing. TypeScript
production generation enables helper sharing only; literal-table sharing is a
private generation-time experiment because its mixed-use measurements do not
justify enabling it by default. The private `GenOptions` controls
`disableMethodSharing`, `disableLiteralSharing` and `enableTSLiteralSharing`
select same-generator outputs without adding public options or runtime checks.
An explicit `disableLiteralSharing` takes precedence over the TypeScript
opt-in.

Direct-rule compilation can fail after speculative lowering. Its snapshot and
rollback journals remove helper and literal entries inserted by a failed path,
together with emitted declarations and reserved names. A later successful path
can emit its own entries; it cannot call a stale entry from rolled-back code.

**Alternatives.** Merging whole rules by apparent semantic similarity risks
changing wrappers, identity, memoization and diagnostics. Exact emitted-body
sharing avoids that broader equivalence. Literal-only sharing leaves duplicate
helpers; helper-only sharing leaves duplicate Go literal data. The TypeScript
literal experiment remains opt-in, separate from production helper sharing.

## Validation and measurements

At measured revision `c87f89a`, the native full test suite, focused
Linux/386 checks, generated freshness and final five-mode parity checks passed.
Those checks describe that revision; the integration checks are listed below. Go and TypeScript controls covered
Node, typed AST and recognition output, both position units, invalid and
non-ASCII inputs, and retained Nodes after pool reuse. A negative rollback
test confirms that failed direct lowering does not leave stale cached entries.

The following measurements use revision `c87f89a`. Five alternating 500 ms
generation rounds on 2026-10-11 used Go 1.27.1 on an
Apple M3 Max (`darwin/arm64`, `GOMAXPROCS=4`). Generation time is the median of
paired ratios, with the minimum and maximum pair shown. The Go production
output (both mechanisms) is compared with the same-source no-sharing control.
TypeScript production (helper sharing only) is compared with that control;
TypeScript literal sharing is excluded from the production result.

| Output | Workload | Source bytes, no sharing → production | Generation time | B/op | allocs/op |
|:--|:--|--:|--:|--:|--:|
| Go | JSON | 210,344 → 205,694 (-2.2%) | 1.024 [0.952–1.032] | 0.984 | 0.975 |
| Go | TypeScript grammar | 5,548,225 → 5,074,974 (-8.5%) | 0.869 [0.846–0.928] | 0.926 | 0.920 |
| Go | DuckDB | 16,590,850 → 14,901,570 (-10.2%) | 0.908 [0.884–0.926] | 0.900 | 0.910 |
| TypeScript | JSON | 146,173 → 140,250 (-4.1%) | 1.001 [0.939–1.076] | 0.959 | 0.985 |
| TypeScript | TypeScript grammar | 4,459,331 → 3,536,967 (-20.7%) | 0.935 [0.892–1.059] | 0.929 | 0.957 |
| TypeScript | DuckDB | 10,179,675 → 5,260,701 (-48.3%) | 0.967 [0.925–1.038] | 0.761 | 0.911 |

Generation time, bytes allocated and allocations are ratios to the no-sharing
control; time ranges are min/max of five paired runs. These fixtures show
smaller output and lower generation allocations, with timing ranges that
often overlap parity. Go fixtures include Node, typed AST and recognition entry points; TypeScript
fixtures include Node and recognition entry points. The table reports the
adopted production defaults, excluding experimental TypeScript literal sharing.

Same-source TypeScript parser measurements explain the production choice.
For the TypeScript grammar's small CodePoints Node parse in the mixed-use
protocol, helper-only sharing was 1.024 [0.910–1.081]
relative to no sharing, while helper-plus-literal sharing was 1.521
[1.496–1.631] and literal-only sharing was 1.715 [1.586–1.816]. The mixed-use protocol warms each small case 1,000 times and large case
three times, then measures 10,000 and ten calls, respectively, in one process.
The isolated confirmation uses a fresh process per input/unit/API, warming
and measuring 30,000 small calls or 300 large calls. A warmed
helper-plus-literal comparison was 1.020 [0.934–1.047] for CodePoints and
0.961 [0.929–0.979] for Bytes. Because the cold/mixed literal-sharing cases
showed substantial risk, TypeScript literal sharing remains a private
generation-time opt-in.

Five paired three-second Go parser runs on two larger workloads compared
production with the same-source no-sharing control. JSON Node Bytes was
0.991 [0.986–1.029] with 138 allocations in both modes; TypeScript-grammar
typed AST Bytes was 1.012 [0.994–1.020] with 6,111 allocations in both modes.
An earlier short screen showed small increases; the longer paired ranges include parity and do not
establish a sustained regression. Generated test-binary sizes were 4,743,858 versus
4,746,002 bytes for JSON and 18,914,002 versus 20,196,162 bytes for the
TypeScript grammar (production versus no sharing). These binaries include the test harness, standard library and debug information;
the sizes do not measure a parser-only executable.

Five rotating fresh-package compile and test-link runs, with dependencies
cached, measured production/control wall-time ratios of 1.007 [0.804–1.096]
for JSON and 0.849 [0.846–0.884] for the TypeScript grammar. This includes
Go command overhead and test linking, rather than pure compiler time or a
cold dependency build. Generation, parser execution and build costs are
separate measurements.

Integration onto `d93132a` independently passed the five-mode Go/TypeScript
workloads, unshared TypeScript corpus, rollback and identity checks, all ten
standalone parser modules (short tests and vet), generated freshness, and site
tests. Three alternating three-second production/control pairs on that tree
measured JSON Node Bytes at 1.000 [0.985–1.039] and TypeScript-grammar typed AST
Bytes at 1.006 [1.001–1.018]. Allocation counts remained 138 and 6,111,
respectively. This screen showed a small 0.6% median time increase for the
latter workload; the optimization primarily reduces source and build costs,
without an established general parsing-speed improvement.

The generated-sharing harness is `TestGeneratedSharingWorkloads` and
`BenchmarkGeneratedSharing` in `internal/engine`. It writes standalone Go and
TypeScript fixtures when `PEGO_GENERATED_SHARING_DIR` is set. The benchmark
controls are selected with `PEGO_GENERATED_SHARING`:

| Mode | Go generation | TypeScript generation |
|:--|:--|:--|
| empty | helper and literal sharing (production) | helper sharing (production) |
| `combined` | helper and literal sharing | helper and literal sharing (experiment) |
| `methods` | literal sharing only | literal sharing only |
| `literals` | helper sharing only | helper sharing only (production) |
| `both` | neither | neither |

For example, generate fixtures and run one measurement mode with:

```sh
PEGO_GENERATED_SHARING_DIR=/tmp/pego-generated-sharing \
  go test ./internal/engine -run '^TestGeneratedSharingWorkloads$' -count=1
PEGO_GENERATED_SHARING=literals go test ./internal/engine -run '^$' \
  -bench '^BenchmarkGeneratedSharing$' -benchmem
```

For paired generation runs, use fresh processes in alternating mode order.
For parser execution and binary-size comparisons, use the emitted `default`,
`literals`, `methods`, `combined` and `both` fixtures as applicable, and keep
generation, compilation, runtime, source bytes and test-binary size separate.
