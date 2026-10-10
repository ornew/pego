# 8. Fast path for unmemoized calls, `SCAN`, nesting limit (d2db87d)

- **Unmemoized calls** skip the memo bookkeeping: the examined range only grows, so it needs no save/restore, and
  expectations need no isolation.
- **Single-character repetitions** (`(?class)*`, `.+`, ...) in value-free positions run as a scan loop without
  per-iteration backtracking state (closure: `scanRepeat`; bytecode: the new `SCAN` instruction).
- **Nesting limit.** Rule-call depth is capped (`WithMaxDepth`, default 100,000; 10,000,000 for the iterative VM).
  This is a robustness fix rather than a speedup: a corrupted `.pegoc` (for example a cleared left-recursion flag)
  used to recurse until the Go stack overflowed.
- Effect: recognition JSON 39 → ~32 ms, CSV 24 → 18 ms, minilang 40 → ~31 ms; full parses a few percent.
