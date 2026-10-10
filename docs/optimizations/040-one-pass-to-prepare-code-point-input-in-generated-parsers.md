# 40. One pass to prepare code-point input in generated parsers

- Generated parsers prepared code-point input in three passes, as the engine did before change 30: converting to code
  points, checking UTF-8 validity, and building the offset table for token text. Full parses always need the table,
  so it is still built eagerly, but now in the same pass as the conversion and the check.
- Effect (min of 16 interleaved runs, Apple M3 Max, generated parsers): JSON 10.1 → 9.6 ms, CSV 4.6 → 4.2 ms, XML 10.1 →
  9.7 ms.
