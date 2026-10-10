# 18. Memoizing from the second call at a position

- Counting memo entries and reuses showed that most entries are never used: JSON stored 68k entries per parse and
  reused none, CSV 30k and none, the Pratt calculator 6.6k and none, minilang reused 1.8k of 46k. Only grammars
  that really backtrack over rules (left recursion: 110k reuses; XML: 12k) use them. Each entry costs 128 bytes,
  its slot, and the isolation of its expectations.
- In whole-input parses (`ParseWith`, including recognition), a memoized rule is now not memoized on its first call
  at a position: a bit set (`memoTable.seen`, one bit per position and per rule that can be deferred) records the
  call, and the rule is memoized when it is called there again. A rule is therefore evaluated at most twice per
  position and parse time stays linear.
- Deferring costs an extra evaluation where a rule is called again. Per rule and per parse, the calls and repeated
  calls are counted, and once more than one call in sixteen is a repeat, the rule is memoized from the first call
  for the rest of the parse (the threshold was one in eight with repeats counted twice until change 24). Without this, the left-recursive calculator was 20–33% slower; with it, it is within
  0–5%. Thresholds of 8 and 32 measured the same.
- Left-recursion leaders are always memoized (the memo drives seed growing). `Document` (which needs every entry for
  reuse after edits) and streams keep memoizing on the first call.
- Consequences found in review: results that had depended on which call was memoized changed. Rule labels on nodes
  returned by actions (fixed: action results are final), errors recovered inside and outside lookaheads (fixed:
  such entries are reused only in the same context), node identity across separate calls (`==` on nodes; now
  stated as unspecified in the specification), and the nesting limit near its value (a memo hit does not nest;
  documented on `WithMaxDepth`).
- Effect (min of 6 interleaved runs, Apple M3 Max, code points; time before → after, bytes per parse in
  parentheses):

  | Workload | closure | bytecode | iterative |
  |:--|:--|:--|:--|
  | JSON | 22.5 → 18.1 ms (41.2 → 25.8 MB) | 26.0 → 22.0 ms | 37.7 → 34.2 ms |
  | CSV | 11.0 → 9.2 ms (31.5 → 22.5 MB) | 12.0 → 10.1 ms | 14.7 → 13.2 ms |
  | XML | 19.3 → 18.2 ms (32.0 → 26.8 MB) | 23.6 → 21.6 ms | 33.4 → 32.4 ms |
  | Arith_Pratt | 13.2 → 12.0 ms (19.4 → 14.2 MB) | 16.8 → 15.7 ms | 25.9 → 24.7 ms |
  | Arith_LeftRec | 33.3 → 35.2 ms (unchanged) | 34.5 → 35.6 ms | 45.8 → 45.7 ms |
  | Minilang | 18.6 → 18.0 ms (20.8 → 18.9 MB) | 24.0 → 24.3 ms | 31.7 → 32.3 ms |

  Recognition gains more, because memo entries were most of its allocations: JSON 15.9 → 12.0 ms and 17.7 → 2.4 MB
  (closure), CSV 6.8 → 4.8 ms and 10.6 → 1.6 MB. (CSV and Arith_Pratt were measured before the per-rule switch was
  added; with no repeated calls the switch never fires on them.)
