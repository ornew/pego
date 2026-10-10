# 11. Generated parsers catch up with changes 4–10

- Generated Go parsers (`internal/engine/genrt` plus the code from `gen.go`) had kept the old runtime: `[]string`
  expected sets copied per memoized call, a Go map for the memo, a heap object per node, child list and frame, and
  token text copied out of `[]rune`. They had become 1.5–2.3× slower than the closure backend while allocating up to
  5× as often.
- Ported to the generated runtime:
  - expected sets as interned IDs; the generator emits the description table (`descs`), and isolated records,
    `keep` with its arena chunks and the reuse of the last kept set work as in the engine (change 4);
  - the memo table with per-position chains and slab-allocated, packed entries; there is no `Document` or stream,
    so no examined range, shift or pruning (change 5);
  - slabs for nodes, child lists and frames, and `kidStack` for repetition children (change 6);
  - the unmemoized fast path in `call`, scan loops for value-free single-character repetitions, and the nesting limit
    (`nesting too deep: more than 100000 rule calls`, the engine's default) (change 8);
  - value-free Pratt lines (`leanLine`), and captures in value-free positions returning no value (changes 1, 9);
  - token text as substrings of the input with a rune-to-byte offset table (copying only for invalid UTF-8), and one
    reused action context per parser (change 10).
- Beyond the engine:
  - character classes are generated as inline range comparisons instead of a loop over a range table, both in
    single matches and in scan loops;
  - Pratt `longest` returns the best attempt by value and saves captures across `reset` in one buffer per parser,
    so trying an operator line without captures allocates nothing (the engine allocates a `prattAttempt` per
    match and a slice per try). On minilang this alone took allocations from 82 k to 19 k per parse.
- `TestGeneratedParsersMatchEngine` now also covers scan loops and the nesting limit (at and one beyond it).
- Effect (generated parser, code points; closure backend in the same run in parentheses):

  | Workload | Time | Allocated per parse | Allocations per parse |
  |:--|--:|--:|--:|
  | JSON | ~110 → ~40 ms (~68 ms) | 53.1 → 35.5 MB (39.8 MB) | 798 k → 54 k (153 k) |
  | minilang | ~152 → ~33 ms (~50 ms) | 51.1 → 17.8 MB (21.5 MB) | 710 k → 19 k (137 k) |
  | CSV | ~71 → ~25 ms (~52 ms) | 28.0 → 26.8 MB (31.3 MB) | 388 k → 46 k (131 k) |
  | XML | ~117 → ~37 ms (~47 ms) | 32.2 → 28.4 MB (30.4 MB) | 490 k → 32 k (64 k) |

  Generated parsers again allocate less than the closure backend (in bytes and in count) and are the fastest
  backend again, about 1.3–2× faster than the closure backend (times are noisy on the shared VM; the closure
  numbers in this run were higher than in earlier entries).
