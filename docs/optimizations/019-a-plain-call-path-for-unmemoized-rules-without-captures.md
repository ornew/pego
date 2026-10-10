# 19. A plain call path for unmemoized rules without captures

- A rule call went through `call` → `invoke` → `invokeBegin` / body / `invokeEnd`: a capture frame was allocated or
  shared and switched to, an `invokeState` was built and copied, and the failure handling of `call` came on top. In
  profiles of recognition this chain was about a fifth of the time.
- An unmemoized call (transient rules, and first calls since change 18) of a rule whose scope has no captures now goes
  through `invokePlain`, which does the same steps inline and leaves the caller's frame current (a body without
  captures never writes one). Depth limit, cut, environment, trail, recovered errors and `finish` behave as before.
  The closure backend and the recursive VM use it; the iterative VM has its own call frames.
- Effect (min of 6 interleaved runs, Apple M3 Max): full parses 2.5–9.5% faster (JSON closure 17.7 → 16.5 ms, bytecode
  21.8 → 19.8 ms; XML closure 17.8 → 16.2 ms), recognition 5.5–15.6% faster (JSON closure 11.8 → 10.0 ms, bytecode
  16.4 → 13.8 ms).
