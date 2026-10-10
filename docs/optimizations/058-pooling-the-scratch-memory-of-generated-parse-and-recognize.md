# 58. Pooling the scratch memory of generated `Parse` and `Recognize`

- Change 57 in the generated parsers' Node runtime: `parse` takes its parser from a `sync.Pool` and returns it
  (`release`), keeping the decoded input and offsets, the memo slots and entry chunks, the bit set of first calls and
  the stacks, and dropping the node, list, field and frame slabs, which the result uses. `Parse` and `Recognize` share
  the pool; the per-rule call counters are dropped when the rule table changes. The runtime now always imports `sync`.
- Effect (min of 6 interleaved runs, Apple M3 Max): `Parse` XML −9%/−12% (code points/bytes), Arith_LeftRec −12%/−15%,
  Recovery −9%/−8%, CSV −8% (code points), Minilang −6%/−5%, JSON within noise (+2%); `Recognize` XML −17%,
  Arith_LeftRec −15%, Minilang −10%, JSON −4% with one allocation per parse; `ParseAST` unchanged (it pooled already).
