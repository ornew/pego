# 35. Left-recursion growth without allocation

- Growing the seed of a left-recursive rule allocated a `growState` per call of the leader and a memo entry on the
  heap for the seed and for every longer result, instead of taking entries from the memo table's chunks: 84k of the
  86k allocations per parse of the left-recursive calculator.
- The grow state is now a value (in the caller's frame, or in the iterative VM's call frame) and the entries come from
  the memo table's chunks.
- Effect (min of 10 interleaved runs, Apple M3 Max, Arith_LeftRec): full parses 6–9% faster (closure 28.9 → 26.6 ms),
  recognition 6–10% faster (closure 21.8 → 19.8 ms); 86k → 2.2k allocations.
