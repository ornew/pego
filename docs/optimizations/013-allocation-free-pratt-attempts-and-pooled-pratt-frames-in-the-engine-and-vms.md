# 13. Allocation-free Pratt attempts and pooled Pratt frames in the engine and VMs

- Ports the generated parsers' Pratt technique (change 11) to the engine and both VMs. `parser.longest` returns the
  winning `prattAttempt` by value instead of allocating one per candidate, and saves captures across a reset in a
  reusable buffer (`parser.saved`) instead of a fresh slice.
- The iterative VM pools its skip, longest, nud and Pratt frames (generic `reuse[T]`, released in `release`), holds
  attempts by value, and pushes no skip frame when the Pratt expression has no `skip`.
- Pitfall: putting the `prattAttempt` into `iresult` (returned on every VM step) made the iterative VM about 20%
  slower on every grammar, Pratt or not. The attempt is passed through `parser.attempt` instead, so `iresult` stays
  small.
- Effect (min of 6, codepoints, allocs/op before → after):

  | Workload | closure | bytecode | iterative |
  |:--|:--|:--|:--|
  | Arith_Pratt | 173k → 134k | 269k → 231k | 484k → 231k; 79 → 67 ms |
  | Minilang | 137k → 75k | 203k → 140k | 274k → 140k; 92 → 75 ms |
  | JSON (no Pratt) | unchanged | unchanged | unchanged; 107 → 101 ms |
