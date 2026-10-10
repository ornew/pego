# 50. First-character dispatch in the closure backend

- Change 49 in the closure backend. The choice peeks at the next character once (which records it as examined, so a
  `Document` still invalidates the result when that character changes) and skips the alternatives that cannot
  start there, recording their expectations. The bytecode VMs are unchanged (it would need an instruction). Rule
  bodies that are skipped are no longer counted in `Document.Stats`, so the counts in the incremental guide changed
  slightly (one fewer evaluation or reuse for a blank line's alternative).
- Effect (min of 12 interleaved runs, Apple M3 Max, closure backend): JSON 14.5 → 13.4 ms (−7%), Minilang −5.5%,
  Recovery −6.5%, XML and Arith_LeftRec −3%; `Document` reparse after an edit 0.95 → 0.72 ms (−23%), since skipped
  alternatives are neither memoized nor looked up; recognition within ±5%.
