# 094. Generation-Only Typed-Output Liveness Cleanup

- **Status**: Proposed
- **Date**: 2026-10-14

## Problem and Mechanism

Generated Go typed parsers allocate values in backing chunks. A small published result can keep unreachable sibling values alive through those chunks. Design 034 evaluates cleanup at the final typed-result publication boundary, after parsing and any Node-to-typed conversion. The marker starts from the result and parse error and traces supported typed values and owned backing cells. A pointer to a scalar field can hide its owning slab object, so the marker promotes that owner and traces its references until the reachable set reaches a fixed point. Only after tracing completes does it zero unmarked slots across tracked blocks, including unused capacity; live cells and slice-capacity-visible cells remain intact.

Two private generation-time variants were evaluated: an unbudgeted full sweep and a bounded sweep. The bounded variant preflights ownership directories and uses fixed work, key, pending-value, owner-record and slot budgets. Parent promotion consumes the bounded work budget. If ownership is unknown or preflight, tracing or promotion exceeds a limit, it falls back to keep-all before clearing any cell. Directory collection during parsing is currently uncapped. The sweep examines whole backing blocks but zeros only unmarked slots; a live cell reached through an interior pointer retains its owner and descendants.

The controls are private and default-off. They affect generated Go typed `ParseAST` and `ParseRuleAST` publication, for direct and conversion-based results, including returned `Match` values and owned backing cells. Generated Node `Parse`, `Recognize`, engine and VM backends, and TypeScript are outside this mechanism. No runtime selector is added and no default allocation policy has changed.

## Applicability

| Execution profile | Applied / Partial / Not applied / Not applicable | Reason |
|:--|:--|:--|
| Generated Go typed results | Partial | Private full-sweep and bounded generation controls; both default-off and still gated |
| Generated Go Node parsing and recognition | Not applicable | No typed-output chunks are published by these paths |
| Engine closure and bytecode VMs | Not applicable | Separate runtime and ownership model |
| Generated TypeScript | Not applicable | No implementation in this runtime |

## Reproduction

See the retained-output fixture and benchmark commands in [design 034](../design/records/034-typed-output-liveness.md). The exact-parent baseline was `d55229fef1c7ade37e9cc208892e0cb20c5a3caa`. A same-generator reference disabled cleanup while preserving generated workload shape; its seven committed parser outputs matched byte-for-byte. The test generator selected cleanup with `enableTypedOutputSweep` or `enableBoundedTypedOutputSweep`; the same-generator reference used neither option. Direct and Node-to-typed conversion comparisons enabled the corresponding option. These controls and fixtures are part of the recorded implementation and can be reproduced from the linked design procedure. Build each benchmark test binary before measuring process RSS, then run only that binary under `/usr/bin/time -l` so compilation is excluded. Do not infer effects for other backends or output paths.

## Results

The ordinary phase used Go 1.27.1 on an Apple M3 Max (darwin/arm64), with `GOMAXPROCS=16`, in 240 fresh-process rows: 16 input/position-unit conditions, three variants (reference, full sweep and bounded sweep), and five alternating samples. Ratios below are ranges of per-condition paired median ratios, not suite-wide speedup claims.

| Variant versus reference | Paired time ratios | B/op ratios | Allocation-count change |
|:--|--:|--:|--:|
| Bounded sweep | 0.988218–1.021221 | 0.998874–1.042010 | +20 to +118 |
| Full sweep | 1.320125–4.024776 | 1.676096–5.218507 | +568 to +20,339 |

These ordinary-workload observations do not determine the default policy. Earlier prototype measurements from other source revisions are summarized only as historical context in [design 034](../design/records/034-typed-output-liveness.md).

The direct generated-typed path was also measured on five shapes in both position units. For an input with an 8-rune prefix of `é` characters plus the fixture suffix, paired median time ratios were 2.056291–3.146894× for bounded sweep and 1.616556–3.025611× for full sweep; bounded added 1,624–1,788 B/op and 4–10 allocations, while full sweep added 503–732 B/op and 4–11 allocations. For an input with a 1,048,576-rune prefix of `é` characters (2 MiB UTF-8) plus the fixture suffix, time ratios were 0.996282–1.035022× (bounded) and 0.994009–1.035215× (full), close to parity. These small-input costs remain a material tradeoff.

In retained-heap probes, the fixture clones every reachable `Text` field before forcing GC. This isolates discarded-sibling backing storage from the input buffer retained by ordinary substring results; it does not show that normal parsing releases the input. For direct parsing with the discard-heavy fixture (`control=false`), the reference retained 2,105,616–2,116,240 B; bounded sweep retained 272–10,912 B and full sweep 272–10,928 B. Three fresh-process peak-RSS samples per shape and position unit showed paired median ratios of 0.980743–1.008368× (bounded) and 0.990291–1.003210× (full). This process-level RSS includes runtime, input and process memory; it is not a live-heap peak measurement and showed no meaningful peak reduction.

The Node-to-typed conversion path was measured separately for the same five shapes and both position units. For an input with an 8-rune prefix of `é` characters plus the fixture suffix, bounded time ratios were 1.165217–1.322767× with 1,443–1,541 additional B/op and 4–7 additional allocations; full-sweep ratios were 1.099241–1.292467× with 320–467 additional B/op and 4–8 additional allocations. For an input with a 1,048,576-rune prefix of `é` characters (2 MiB UTF-8) plus the fixture suffix, bounded time ratios were 0.987805–1.005104× with 1,428–1,587 additional B/op and 4–7 additional allocations; full-sweep ratios were 0.985538–1.006372× with 312–457 additional B/op and 4–8 additional allocations. In conversion retained-heap probes using the same `Text`-cloning fixture, reference, bounded and full-sweep variants all retained 272–1,232 B. Conversion peak-RSS paired median ratios were 0.993103–1.013966× (bounded) and 0.991826–1.009709× (full), showing no meaningful reduction. Direct-path memory results therefore do not transfer to conversion results.

Across direct and conversion paths, 1,620 measurement rows covered ordinary workloads, size-controlled inputs, retained heap and peak process RSS. Timing used five alternating 500 ms samples; heap probes used five fresh one-shot runs; RSS used three fresh processes per shape and unit. Ranges are descriptive paired observations, not statistical confidence intervals, and ordinary host activity may add noise. Focused correctness checks, full root tests, generated parser checks, parser module tests, and an independent source review passed for this candidate. Seven default outputs remained byte-identical. The memory controls remain off: the small-input costs are material, bounded cleanup adds ordinary-workload allocations, and conversion results show no retained-heap or peak-RSS reduction. No broad speedup is established.

The implementation must preserve pointer identity, aliases, cycles, values, slice headers and all backing cells visible through published capacity. It must complete graph tracing and parent promotion before clearing, must not run during backtracking or action evaluation, and must retain all storage when ownership cannot be proven. Full sweep has no explicit work budget; bounded sweep can fall back to retaining all values on budget exhaustion and still incurs uncapped directory-collection cost. The sweep examines unused slots too, but does not clear a cell reachable through a result, including an owner reached through an interior scalar-field pointer.

## Decision and Limits

Not adopted; both controls remain disabled by default. The measurements show substantial direct-path retained-heap reduction but material small-input latency and allocation costs, plus no conversion-path retained-heap or peak-RSS reduction. Revisit only if a revised design meets default-path cost limits while retaining meaningful benefits across direct and conversion results, returned matches, both position units, and ordinary and discard-heavy inputs. Broader runtime and TypeScript behavior remain unmeasured and out of scope.
