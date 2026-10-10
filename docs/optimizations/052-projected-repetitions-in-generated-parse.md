# 52. Projected repetitions in generated `Parse`

- The projection of change 48 (a repetition captured only to be mapped to one field of its elements gathers that
  field directly) now applies to the generated parsers' Node runtime too. A review of the typed runtime found the
  conditions incomplete, and they are now: the action reads the capture only as `map($x, (e) => $e.f)` and uses no
  `$n` (which can reach the same list), the names involved are captured once in the rule, and `f` is captured in
  every element with a value that is never nil (otherwise an element without a record makes `$e.f` an error).
- Effect (min of 8 interleaved runs, Apple M3 Max, generated `Parse`): CSV 3.9 → 2.7 ms (−30%, 14.0 → 8.7 MB), JSON
  7.8 → 7.0 ms (−10%, 18.4 → 15.1 MB), Minilang −4%; grammars without such repetitions unchanged.
