# 27. Struct and Call Operands on the Expression Stack in the Closure Backend

- The closure backend's evaluator built two slices for every `T{...}` (field names and values) and one for every
  built-in call (arguments). Values and arguments now go on the parser's expression stack (`parser.estack`, as in
  the VMs since change 14) and field names into a reused buffer; `newStruct` and the built-ins retain neither.
- Effect (min of 8 interleaved runs, Apple M3 Max, closure backend): Arith_OperatorsRule 11.1 → 10.6 ms, 40.6k → 0.6k
  allocations, 14.2 → 12.2 MB; XML 38.9k → 19.1k allocations; minilang 8.3k → 1.1k allocations; times otherwise
  within noise. All backends now allocate fewer than 1.5k objects per parse on JSON, CSV, Operators Rule and minilang; XML's
  remaining 19k are `text(...)` results (a string stored in an interface needs a heap header).
