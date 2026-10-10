# 5. Memo table with per-position chains (cf77e4f)

- The memo was a Go map keyed by (rule, position, level): hashing, map growth and one allocation per entry.
- Entries now hang off a slice indexed by position and are allocated in slabs of 256. Stream pruning drops a prefix of
  the slice. Kept expectation sets are copied into fixed-size chunks, and a repeat of the previous set is shared.
- Effect: JSON ~130 → ~85 ms, minilang ~103 → ~65 ms; about a third of parse time.
