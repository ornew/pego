# 45. Moving reused trees in place after document edits

- A `Document` reparse copied the node tree of every reused result after an edit with shifted positions
  (`shiftNode`), so it allocated in proportion to everything after the edit. Nodes are now moved in place
  (`moveResult`): each node records how many document edits its positions account for (`Node.gen`, in padding, so
  `Node` stays 120 bytes), and the document keeps its edits. A non-empty node moves by the edits that lie before it,
  which its positions tell; empty nodes at an insertion point are ambiguous and are copied instead. Edits that keep
  the length no longer mark entries as shifted.
- A review found that the first version collapsed the descendants of an empty result (captures made in a
  lookahead) onto one point, and that it replayed every edit since the last parse for every node, so a parse after
  5,000 edits took 56 ms instead of 0.7 ms on 20,000 lines. Empty nodes now move their descendants by the same
  shift, and a memo entry's own accumulated shift (with the generation at which its result was last current) gives
  the shift of most nodes; edits are replayed only for nodes moved through another result since, once per
  generation.
- Trees returned by earlier parses now change when the document is parsed again; `Node.Clone` keeps a copy.
- Effect (min of 12 interleaved runs, Apple M3 Max, minilang, one-character edit and reparse): 1.34 → 0.90 ms on the
  closure backend, 1.30 → 0.92 ms (recursive VM), 1.24 → 0.94 ms (iterative VM); 2.4 MB → 0.34 MB and 151 → 49
  allocations per edit. Most of what remains is the `madvise` calls of the Go runtime reusing scavenged memory, which
  is specific to macOS, and `memoTable.splice` walking every memo entry.
