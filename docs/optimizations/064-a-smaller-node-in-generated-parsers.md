# 64. A smaller `Node` in generated parsers

- Change 63 in the generated Node runtime, whose `Node` is a type of the generated package: the kinds of a rule's nodes
  are set up in `init` with the rule table, those of struct types with `structFields`, and the emitted code makes CST
  nodes with the shared kinds of the reserved types. The typed converters read `Type()` and convert positions to the
  `int` spans of the typed values.
- Effect (min of 6 interleaved runs, Apple M3 Max): `Parse` JSON −2%/−3% (code points/bytes), CSV −8%/−12%, XML
  −7%/−5%, Arith_Pratt −3%/−4%, Arith_LeftRec −2%/−4%, Minilang within noise, Outline and Recovery +1% to +5%
  (noise-level); allocation −10% to −21% (JSON 12.8 → 10.7 MB, Outline 12.8 → 10.2 MB); `ParseAST` and `Recognize`
  unchanged within noise.
