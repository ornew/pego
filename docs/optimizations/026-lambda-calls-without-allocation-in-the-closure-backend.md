# 26. Lambda calls without allocation in the closure backend

- Each lambda call (`closure.apply`) copied the evaluation context and allocated a `local` for each parameter, and
  each lambda expression allocated a `closure`. On JSON this was 36k of the closure backend's 37k allocations per
  parse.
- Contexts and parameters of a call now come from LIFO arenas in the parser (`arena[T]`), freed when the call
  returns; closures come from an arena freed at the next evaluation (`useCtx`), since a lambda cannot outlive the
  action or predicate that created it (lambdas are only arguments of `map`, `foldl` and `foldr`, and functions
  cannot be stored). A lambda created inside another lambda's call keeps a heap copy of that call's context (`keep`).
- Effect (min of 8 interleaved runs, Apple M3 Max, closure backend, code points): JSON 15.9 → 15.3 ms, 37k → 1.3k
  allocations, 23.6 → 21.4 MB; CSV 8.0 → 7.3 ms, 56k → 0.8k allocations, 19.0 → 15.6 MB; minilang 21k → 8k
  allocations. XML and Arith_Pratt use no lambdas and are unchanged.
