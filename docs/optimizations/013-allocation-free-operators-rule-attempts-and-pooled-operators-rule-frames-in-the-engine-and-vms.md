# 13. Allocation-Free Operators Rule Attempts and Pooled Operators Rule Frames in the Engine and VMs

- Ports the generated parsers' Operators Rule technique (change 11) to the engine and both VMs. `parser.longest` returns the
  winning `operatorsRuleAttempt` by value instead of allocating one per candidate, and saves captures across a reset in a
  reusable buffer (`parser.saved`) instead of a fresh slice.
- The iterative VM pools its skip, longest, nud and Operators Rule frames (generic `reuse[T]`, released in `release`), holds
  attempts by value, and pushes no skip frame when the Operators Rule has no `skip`.
- Pitfall: putting the `operatorsRuleAttempt` into `iresult` (returned on every VM step) made the iterative VM about 20%
  slower on every grammar, Operators Rule or not. The attempt is passed through `ivmMachine.attempt` instead, so `iresult` stays
  small.
- Effect (min of 6, codepoints, allocs/op before → after):

  | Workload | closure | bytecode | iterative |
  |:--|:--|:--|:--|
  | Arith_OperatorsRule | 173k → 134k | 269k → 231k | 484k → 231k; 79 → 67 ms |
  | Minilang | 137k → 75k | 203k → 140k | 274k → 140k; 92 → 75 ms |
  | JSON (no Operators Rule) | unchanged | unchanged | unchanged; 107 → 101 ms |
