# 49. First-character dispatch in generated choices

- In generated parsers, an alternative of a choice that must begin with a given terminal (a literal or a
  character class, possibly behind sequences, captures, `@`, `-` and calls of rules that are neither
  left-recursive nor Pratt) is skipped when the next character cannot start it, recording the expectation it
  would have recorded. Such an alternative fails at once without its terminal, so nothing else is observable; the
  calls it skips would only have set memo bookkeeping, and the skip is not taken when those calls would exceed the
  nesting limit. `value = object / array / string / number / bool / null` in JSON, for instance, no longer calls
  four rules to parse a number.
- Effect (min of 12 interleaved runs, Apple M3 Max): `Parse` JSON 9.1 → 7.9 ms (−13%), XML −10%, Minilang and
  Recovery −9%, Arith_Pratt −6%, CSV −4%; `ParseAST` JSON 7.3 → 6.4 ms (−12%), XML −10%, Arith_Pratt −11%; fewer
  bytes and allocations (fewer memo entries). `Recognize` uses the same code.
