# 4. Expected sets as interned IDs on a shared stack (11a6ce3)

- Error reporting records what was expected at the farthest failure. Every memoized call and every `#error`/`#recover`
  region used to start a fresh `[]string`.
- Expectations are now compile-time IDs into a per-backend description table. Isolated regions are windows of one stack;
  only sets kept by the memo or a pending recovery are copied into an append-only arena.
- Effect: minilang 59 → 42 MB, ~128 → ~103 ms. Error messages are identical.
