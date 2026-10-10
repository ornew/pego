# 30. Building the code-point offset table on demand

- In code-point mode, preparing the input made three passes over it: converting it to code points, checking that it
  is valid UTF-8, and building the code-point-to-byte offset table that token text uses (change 10). In profiles of
  recognition this was about 4.5% of the time, though recognition rarely needs token text.
- The input is now decoded and checked in one pass, and the offset table is built the first time `text` needs it
  (and before a `Document` edit splices it).
- Effect (min of 6 interleaved runs, Apple M3 Max): recognition JSON 9.6 → 9.1 ms (closure), 13.6 → 12.9 ms
  (bytecode), CSV 4.2 → 3.8 ms (closure), 5.2 → 4.8 ms (bytecode), with 1 MB less allocated per parse; full parses and
  XML recognition (whose predicates read token text) unchanged within noise.
- Pitfall: `Document.Parse` gives the parser a copy of the input, so the table built during a parse was lost and every
  edit and every reparse of a `Document` rebuilt it from the whole text (an edit to a 100,000-line document went from
  0.3 to 2.2 ms). The incremental benchmarks were not in the measurements of this change. The `Document` now keeps
  the tables a parse builds (`TestDocumentKeepsInputTables`).
