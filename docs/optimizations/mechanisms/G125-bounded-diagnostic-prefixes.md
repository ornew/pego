# G125: Bounded Lint Diagnostic Prefixes

Lint diagnostics show a shortened expression when its formatted text is long.
The display step retains only the displayed prefix and a bounded lookahead
instead of copying the full formatted string into a rune slice.

## Mechanism and Invariants

Formatting still runs first. The display step stores at most 37 runes and reads
through the 41st rune to determine whether the existing truncation rule applies:
expressions of at most 40 runes remain unchanged, while longer expressions show
the first 37 runes followed by `...`. On truncation, rune conversion preserves
the previous replacement of invalid UTF-8 in caller-built ASTs; untruncated
text remains verbatim. This bounds prefix
processing and temporary prefix storage; it does not bound formatter work or
the size of the formatted string.

## Scope and State

This applies to lint diagnostic messages in CLI and LSP use. It is independent
of parser execution and all six runtime backend cells are not applicable. The
default lint path uses the bounded prefix.

## Reference Control

No maintained same-source control exists for the former full rune-slice copy.

## Evidence and Implementation

The combined formatter and lint cancellation measurements in [DR042](../../design/records/042-responsive-lsp-analysis-and-code-actions.md#grammar-and-lsp-formatting-cancellation)
include these diagnostic paths, but do not isolate the prefix change. They are
not evidence of an isolated speedup, peak-memory reduction or latency bound.

Source owners: [`showControlled`](../../../internal/lint/analysis.go) and
[`formatting benchmark`](../../../internal/lint/format_context_bench_test.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G120](G120-lsp-position-checkpoints.md).

[Optimization Catalog](../README.md)
