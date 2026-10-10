# 57. Pooling the scratch memory of whole-input parses

- Each `Parse` allocated the decoded input and its offset table, the memo table (slots, entry chunks, the bit set of
  first calls) and the stack that collects repetition values, and dropped them when it returned: nothing in a result
  refers to them (node text slices the input string, errors are built before returning). A `Program` now keeps them
  in a `sync.Pool` (`newPooledParser`, `releaseScratch`): the memo's slots and entry chunks are cleared and reused,
  and buffers over 4M elements are not kept. Recognition uses its program's pool. `Document` and streams keep their
  own memory as before.
- Effect (min of 6 interleaved runs, Apple M3 Max, code points): full parses −1% to −8% (Minilang −8%/−7%/−6% on the
  three backends, XML −6%/−5%/−4%, JSON −2%/−1%/−1%), allocation −14% to −38% (Minilang 15.3 → 9.5 MB); recognition
  −5% to −11% (Arith_LeftRec −11%, 24.7 → 3.4 MB).
