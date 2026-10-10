# 63. A smaller `Node`

- `Node` was 120 bytes: two name strings (`Type`, `Rule`), `int` positions, the text, children, fields and flags. The
  pair of names now sits behind one pointer to an interned kind (`nodeKind`; rules precompute the kinds they give
  their nodes, and programs those of the grammar's types, so a parse never looks one up), and `Start` and `End` are
  `int32`: 88 bytes. This changes the API: `Type()` and `Rule()` are methods, and positions are `int32` (Go accepts
  them as slice indexes; arithmetic with `int`s needs a conversion). JSON and `String` output are unchanged
  (`MarshalJSON` writes the same members).
- Measured beforehand with a synthetic tree of 400,000 nodes: building −15%, node memory −22%, walking −7%, a GC with
  the tree live unchanged; `int32` positions alone saved nothing, since a chunk of 256 nodes of 112 bytes falls into
  the same allocation size class as one of 120 bytes.
- Effect (min of 6 interleaved runs, Apple M3 Max): allocation per parse −11% to −20% (JSON 13.0 → 10.8 MB, XML 16.4 →
  13.5 MB, CSV 7.1 → 5.7 MB, streams 168 → 152 MB); time within ±3% on every workload and backend.
