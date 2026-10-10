# 42. Generated calls of plain rules

- Generated parsers called every rule through `call`, which checks whether the rule is memoized before reaching
  `invokePlain` for rules that never are (change 21). The generator knows which rules those are (`rule.plain`,
  change 38; generated parsers have no `Document`), so their call sites now call `invokePlain` directly.
- Effect (min of 12 interleaved runs, Apple M3 Max, generated parsers): JSON 9.7 → 9.3 ms, XML 9.7 → 9.1 ms, others
  0–2% faster.
