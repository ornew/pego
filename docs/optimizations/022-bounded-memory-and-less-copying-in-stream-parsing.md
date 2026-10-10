# 22. Bounded memory and less copying in stream parsing

- **Retention.** A stream parse is meant to hold only its working set, but the reachable heap grew with the number of
  elements on several grammars (found while writing the streaming guide): about 78 MB per 100,000 records for a grammar
  with a header and captured fields, on every backend. Nodes, child lists, frames and field lists come from chunks
  (change 6), a chunk stays alive while anything in it is referenced, and it keeps alive what its objects point to.
  Consecutive elements shared chunks, so the chunk being filled reached the previous element's chunks, and so on back
  to the first element. At an element boundary, once the node chunk is nearly used up or the elements since the last
  split filled more than one chunk, the parser now starts new chunks of every kind (`splitChunks`), so chains stay
  within a group of elements. `TestParseStreamMemoryIsBounded` checks the reachable heap on every backend; it fails
  without the split.
- **Copying.** `discard` copied the whole read-ahead buffer into a new slice at every committed element. It now
  compacts in place, and only once at least half of the buffer can go.
- Effect (min of 6 interleaved runs, Apple M3 Max): streaming 50,000 CSV records 168.9 → 117.7 ms (closure), 180.6 →
  129.9 ms (bytecode), 215.3 → 160.8 ms (iterative), and 686 → 351 MB allocated per parse. The reachable heap no longer
  grows: 233 MB → 0.5 MB after 300,000 records of the grammar above. A 27 MiB, 1,000,000-record CSV file now streams with
  a peak resident size of about 61 MB (0.8 GB before; a whole-input `Parse` takes 2.2 GB).
