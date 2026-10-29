# 098. Bootstrap Generated Typed Output Allocations

- **Status**: Adopted
- **Date**: 2026-10-14

## Problem and Mechanism

Generated Go typed parsers previously placed early objects and list backing arrays into slabs. A tiny parse could retain or touch much more storage than its result needs. The bootstrap allocates the first three objects or terminals for each owner independently, then resumes the existing object-slab growth from 8 to 256 values. It allocates each owner's first nonempty small list independently, then resumes the existing list-slab growth from 64 to 1,024 elements. The later slab policy and maximum sizes are unchanged.

The bootstrap applies during generated Go typed output construction, both for direct typed parsing and for Node-to-typed conversion. A generation-only `disableTypedOutputBootstrap` reference emits the legacy allocation path. Full and bounded typed-output sweep variants keep the legacy helpers, so this mechanism is measured separately from records [074](074-smaller-first-chunks-for-generated-typed-values.md) and [094](094-generated-typed-output-liveness.md). It does not change matching, ordinary Node parsing, recognition, TypeScript output or engine/VM execution.

## Applicability

| Execution profile | Status | Scope |
|:--|:--|:--|
| Generated Go typed `ParseAST`, direct construction | Applied | First three objects and first nonempty small list bootstrap independently before existing slabs |
| Generated Go typed `ParseAST`, Node conversion | Applied | Same allocation policy during conversion |
| Generated Go Node `Parse` and `Recognize` | Not applicable | No typed output allocation |
| Generated TypeScript | Not applicable | No change |
| Closure, bytecode VMs and iterative VM | Not applicable | No change |

## Reproduction

The adopted policy was compared with the legacy slab policy on Go 1.27.1 and an Apple M3 Max (`GOMAXPROCS=16`, `GOGC=100`, `GOMEMLIMIT` unset). Focused JSON `ParseAST` controls used five ABBA pairs with 500 ms per sample; the generated workload controls used four ABBA pairs with 300 ms per sample across five grammars and both input units. Retained-heap controls used three alternating fresh-process probes across four shapes and both units. Raw logs remain outside the repository. The same-generator legacy comparison retains the new bootstrap field in its allocator state, so it is not layout-identical to historical main; focused comparisons against main provide additional context.

The retained-output fixture exports direct and conversion variants, including the `slabs` default and `legacy` reference. Its heap probe clones every returned `Text` before collection and excludes the input owner from the measured retained result; this isolates discarded-sibling retention and does not show that an ordinary parse releases its source string:

```sh
PEGO_TYPED_OUTPUT_RETENTION_DIR=/tmp/retention \
  go test ./internal/gen/gogen -run '^TestGeneratedTypedOutputRetentionFixture$' -count=1
```

Measure retained output separately for each exported variant:

```sh
for variant in slabs legacy; do
  for path in "${variant}" "${variant}-conversion"; do
    (cd "/tmp/retention/$path" && \
      go test -run '^$' -bench '^BenchmarkTypedOutputRetainedHeap$' \
        -benchmem -benchtime=1x -count=1)
  done
done
```

For ordinary generated typed workloads, export same-generator controls with the exporter that is in the tree up to commit `0a3ef83f^`; `ref` is the default and `legacy` is the generation-only allocation reference:

```sh
PEGO_TYPED_OUTPUT_SWEEP_EXPORT_DIR=/tmp/ordinary \
  go test ./internal/engine -run '^TestExportOrdinaryTypedSweepBenchmarks$' -count=1
```

Build both benchmark binaries before timing, apply the generated `legacy` and `ref` overlays from the export, then run the selected `generated_ast` cases serially with the same inputs and alternating order. The full ordinary inputs include JSON, XML, Minilang, Outline and Recovery. Do not compare only aggregate benchmark runs as a causal before/after result.

## Results

In final exact-source measurements, the discard-heavy direct typed fixture retained about 2.105–2.111 MB per parser version with legacy slabs versus 0.096–5.608 KB with the bootstrap policy across terminal, structure, list and slice results in both position units. The conversion fixture retained mostly 0.3–1.2 KB with legacy slabs and mostly at most 0.7 KB with bootstrap; occasional 5–10 KB fluctuations are consistent with GC noise. This provides no meaningful conversion-path retention reduction. The direct heap result applies only to the cloned-Text fixture described above.

Five JSON `ParseAST` ABBA pairs showed `SmallParseAST` at 0.867× main time in CodePoints and 0.893× in Bytes, with about 1,226 fewer B/op and five additional allocations per operation. The larger JSON `ParseAST` workload was about 0.995× main time, with roughly 1,010 more B/op and 23 additional allocations. In four same-generator ABBA pairs across ordinary `generated_ast` cases, time ratios were JSON Bytes 1.003 (range 0.988–1.024), JSON CodePoints 1.006 (0.985–1.031), XML Bytes 1.023 (1.010–1.039), XML CodePoints 1.013 (1.002–1.029), Outline 1.002–1.012, Minilang about 1.000, and Recovery 0.985–0.997. The separate old-main XML Bytes overlay measured 1.036 (1.025–1.042). Allocation counts increased by 4–80 per operation in measured cases; B/op was noisy and multimodal. Standalone generated parser source grew by 268 bytes each for JSON, XML and Minilang (JSON 381,377→381,645 B; XML 1,095,835→1,096,103 B; Minilang 456,135→456,403 B). These measurements are workload-specific and do not establish a general speedup.

## Correctness and Tradeoffs

The bootstrap changes only where early values and list backing arrays are allocated. It leaves later slab sizes, parse results and conversion policy unchanged. The generation-only reference makes the legacy allocation strategy directly comparable. The retained-heap fixture is intentionally discard-heavy; ordinary conversion results already retain little memory, and the real JSON timings show that fewer retained siblings do not imply uniformly lower allocation traffic or faster parsing.

## Decision and Limits

The default bootstrap was adopted for its direct-result retention reduction. The XML Bytes default-path slowdown and additional allocations are accepted and recorded above. The conversion-path reduction is small in absolute terms, and the full and bounded sweep controls remain disabled by default. The generated source, root and parser-module tests, and independent correctness review passed. A full 199-profile benchmark snapshot (three samples per profile) also passed locally; it is a cumulative snapshot rather than a causal comparison and is not committed as raw output. Keep direct construction and Node conversion separate in future evaluations.
