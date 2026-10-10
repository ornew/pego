# 15. Allocation-free action plumbing in all backends

- With change 14 the VMs' remaining allocations were mostly in code shared with the closure engine, around actions
  rather than in the actions' own values:
  - an action whose body is not a sequence got its element list (`$1`) as a fresh one-element slice
    (`finish`, `lineResult`); on JSON this was over a third of all allocations;
  - field lists of struct nodes (`newStruct`) and of captures (`attachCaptures`) were separate `make` calls;
  - `function.apply(args ...any)` is an interface call, so the variadic argument slice escaped on every lambda call;
  - `list`, `map` and `concat` built their element slices with `make`/`append` and `newList` then copied them;
  - a Pratt operator action allocated a `local` for each of `$op`, `$lhs`, `$rhs`.
- Now: the one-element list is a buffer in the parser (`parser.one`; the list is only read during the action, and `$0`
  copies it); field lists come from chunks like nodes (`parser.fields`, with capacity limited to the list so an
  append past it reallocates instead of overwriting a neighbour); `apply` takes two arguments; the list built-ins
  gather elements on `kidStack` and hand the copied-out slice to the list node; the operator locals live in the
  parser (`parser.oplocals`), like the evaluation context.
- Effect (min of 6 interleaved runs, codepoints, Apple M3 Max; allocs/op before → after, time in parentheses):

  | Workload | closure | bytecode | iterative |
  |:--|:--|:--|:--|
  | JSON | 153k → 38k (22.7 → 22.0 ms) | 147k → 32k (27.2 → 26.5 ms) | 147k → 32k (38.3 → 38.1 ms) |
  | CSV | 131k → 56k (11.3 → 10.8 ms) | 116k → 41k (12.9 → 12.3 ms) | 116k → 41k (15.7 → 15.2 ms) |
  | XML | 64k → 39k (19.0 → 19.2 ms) | 72k → 47k (24.8 → 24.0 ms) | 72k → 47k (34.4 → 34.0 ms) |
  | Arith_Pratt | 134k → 41k (14.1 → 12.9 ms) | 94k → 0.7k (17.8 → 17.0 ms) | 95k → 0.8k (27.0 → 26.4 ms) |
  | Minilang | 75k → 21k (19.0 → 18.2 ms) | 68k → 14k (24.6 → 23.9 ms) | 68k → 14k (32.3 → 31.6 ms) |

  Bytes per parse drop by 2–13% (XML +0.3%: partly used field chunks).
