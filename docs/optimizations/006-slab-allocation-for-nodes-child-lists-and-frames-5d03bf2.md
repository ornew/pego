# 6. Slab allocation for nodes, child lists and frames (5d03bf2)

- Nodes, child slices and capture frames come from per-parser chunks, in the spirit of an arena: a chunk stays alive
  while any of its nodes is referenced (the lifetime of the parse result). Repetition children are gathered on a
  shared stack and copied out once. Memo entries were packed into 128 bytes.
- Effect: allocations per parse dropped by about three quarters (JSON 940 k → 250 k); time −5 to −10%.
