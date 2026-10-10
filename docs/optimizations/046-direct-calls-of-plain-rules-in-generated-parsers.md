# 46. Direct calls of plain rules in generated parsers

- Generated parsers called rules that are never memoized and have no captures (`rule.plain`) through
  `invokePlain`, which runs the body through the rule's function value (a closure that calls the body's method).
  The generator now writes a method per such rule (`r<id>`) that does what `invokePlain` does and calls the body's
  method directly; for a rule without a value it also skips `finish`. Pratt rules still use `invokePlain`.
- Effect (min of 20 interleaved runs, Apple M3 Max): 0.3–6% faster on every workload (JSON 9.36 → 8.80 ms, XML
  9.22 → 8.95 ms). Generated files grow by a few lines per such rule.
