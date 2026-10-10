# 61. Reading code points directly in direct rules

- After change 60, `peek` (the character at the position, in either position unit) was 9% of a JSON `ParseAST`
  profile. The compiler does not inline it: its cost is 110 against a budget of 80, mostly for decoding a multi-byte
  character in `Bytes`, and still 88 with that moved into a function of its own, because the call alone costs 57.
  Direct rules now read the character from `p.in[p.pos]` while `p.pos < len(p.in)`, and call `peek` otherwise, in
  character classes, `.`, scan loops and the first-character tests of choices. `p.in` is empty in `Bytes`, which
  `run` now ensures rather than relying on how a pooled parser was cleared.
- Effect (min of 8 interleaved runs, Apple M3 Max, `ParseAST`): JSON 3.75 → 3.30 ms (−12%), XML 4.41 → 3.99 ms
  (−10%), Outline −6%, Arith_LeftRec −3%, Arith_Pratt −2% (its Pratt loop is the general code).
