# 54. Resuming long repetitions in a `Document`

- A reparse runs again the rule that contains the edit, and any repetition in it ran again element by element: for a
  file's `line*`, a memo lookup per line. A profile of a 100,000-line settings file put half of the reparse in that
  loop (`callBegin`, memo lookups). A repetition of 16 elements or more now records its run in a `Document` parse
  (per element: positions, examined range, expectations, value); after one edit it reuses the elements before the edit,
  parses from there, and once an element ends where an old element after the edit began, reuses the rest of the old
  run with its nodes moved in place (design 007, "Resuming repetitions"). The old run's array is updated in place, and
  the stack that collects repetition values is kept by the `Document` across parses instead of growing from empty
  each time (that growth alone was 4.3 MB per reparse at 100,000 lines).
- Effect (min of 12 interleaved runs, Apple M3 Max, closure backend): one-character insert/delete in the middle of a
  100,000-line settings file 11.0 → 5.1 ms (−54%, 8.3 → 3.8 MB); `BenchmarkIncrementalLong` (a new benchmark: a
  50,000-record CSV) 15.1 → 8.4 ms (−44%); the guide's same-length edit at 100,000 lines 9.2 → 0.6 ms for the reparse.
  Minilang `Document` +2% (its repetitions are short and are recorded only when they reach 16 elements, but every
  element pays for keeping its examined range apart); batch parses unchanged. The records cost about 80 bytes per
  element of a long repetition (Document heap at 100,000 lines 143 → 151 MiB).
- What remains of an edit at 100,000 lines is `Edit` itself (the memo splice visits every entry, about 30% of the
  insert/delete benchmark), moving the nodes after an edit that changed the length, and the new list of children.
