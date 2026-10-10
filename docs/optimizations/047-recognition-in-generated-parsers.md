# 47. Recognition in generated parsers

- Generated parsers had no recognition mode. `pego gen -recognize` (`pego.WithRecognize()`) now also generates the
  recognizer (`Program.recognizer`, the program in which no rule builds a value) into a second rule table, behind
  `Recognize(input) error`. The generator follows the engine there: captures that no predicate reads are not
  recorded, and Pratt lines of rules without a value build nothing (the generated runtime's `lineResult` and
  `prattBuild` now check `novalue`, as the engine's do).
- Effect (min of 3 runs, Apple M3 Max): JSON 4.9 ms against 8.8 ms for `Parse` and 8.6 ms for the closure backend's
  recognition; CSV 1.8 ms (`Parse` 4.0 ms), XML 7.3 ms (8.9 ms), Outline 1.7 ms (3.9 ms). The parity test checks that
  `Recognize` returns the errors of the engine's recognition on every corpus input.
