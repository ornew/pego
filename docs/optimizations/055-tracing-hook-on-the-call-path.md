# 55. Tracing hook on the call path

- Tracing (design 014) adds one `p.tr != nil` test at the entry of `parser.call` and one on the iterative VM's
  non-plain call path. The plain call sites test `p.noPlain` instead of `p.memoAll` (same cost); `noPlain` is set by
  `Document` and by tracing, so a traced parse sends every call through `call`. Nothing else on the untraced path
  changes, and generated parsers are untouched.
- Effect (min of 12 interleaved runs, Apple M3 Max): JSON and Minilang, closure and recursive VM, −1.3% to +0.3%
  (noise); a first 8-run pass showed up to +6% that did not reproduce. Incremental and stream benchmarks within ±4%
  in both directions.
