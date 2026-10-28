# 123. Smaller Boilerplate in Generated Go

- **Status**: Implemented
- **Date**: 2026-10-29

## Problem and Mechanism

Five forms repeated once per rule, per typed node or per parser in every generated Go parser, and
each carried tokens that said nothing.

**Rule table defaults.** A `RuleSpec` holds `Seen` plus one, so its zero value means a rule without
a deferred memoization index and the generator leaves the field out; the entry for the rare fields
(`Where`, `NodeType`, `OpNodeType`) prints only the fields that are set. `NewRules` subtracts the
one when it builds the rules. Printing every field, defaults included, costs a `Seen: -1` and a
zero-valued rare-field entry per rule.

**Rule bodies.** The function of a rule body has the signature of the `Body` field
(`func eM(p *Parser, _ int) (*Node, bool)`), and `init` assigns it (`rules[N].Body = eM`); the
generated callers of a body pass level 0. A wrapping closure per rule,
`rules[N].Body = func(p *Parser, _ int) (*Node, bool) { return eM(p) }`, costs a closure frame on
each dispatch unless the callee is inlined into it. Only the functions of rule bodies take the
level, so no other call site changes, and the runtime keeps one field and one dispatch. The
Operators Rule keeps a `Body` closure because it reads the level.

**Typed constructors.** A constructor ends with one call, `c.MadeOf(v, f0, f1, ...)`, that folds the
spans of the fields. One `FieldSpanOf(fN, &first, &start, &end)` call per field instead keeps three
address-taken locals alive and repeats a line per field. The variadic argument does not escape
(`go build -gcflags=-m`), so no slice is allocated.

**Rule table entries.** The generated package declares `ruleSpec` and `ruleExtra` with the layout of
`RuleSpec` and `RuleExtra` and writes the entries positionally (`{"ws", &sh2, rt.RuleMemo, 1, 0, 0}`)
rather than keyed (`{Name: "ws", Value: &sh2, Flags: rt.RuleMemo, Seen: 1}`). `go vet` rejects
unkeyed fields of an imported struct type but accepts them for a type of the package, and
`NewRules` is generic over the entry types, so the table stays static data and no per-entry code
runs at start.

**Chunk helpers.** The generic helpers that hand out the values of a typed result from chunks live in
the runtime as `Chunk`, `Alloc` and `AllocSlice`, with the bootstrap behavior of the generated form,
instead of being printed into each typed parser. They are functions, not methods: a method on a
generic type adds a method table to the type descriptor of every instantiation, which costs the
duckdb binary 23 KB. The separate-allocation, no-bootstrap and plain-slab variants that serve as
measurement references print their own helpers under local names.

## Results

Sizes on darwin/arm64 (Go 1.27.1, `-trimpath`). `parser.go` is the generated source in bytes; the
binary is a main that links the parser and parses one input, stripped with `-s -w`.

| Parser | Before | Table defaults | Direct bodies | Constructors | Positional table | Chunk helpers |
| --- | --- | --- | --- | --- | --- | --- |
| json parser.go | 60,672 | 60,222 | 59,662 | 59,360 | 59,100 | 58,324 |
| cel parser.go | 116,722 | 115,944 | 115,944 | 114,192 | 113,637 | 112,886 |
| python parser.go | 625,454 | 622,022 | 622,022 | 611,306 | 603,667 | 603,097 |
| duckdb parser.go | 1,650,083 | 1,638,203 | 1,638,203 | 1,607,011 | 1,588,592 | 1,588,337 |
| json binary | 2,200,882 | 2,200,882 | 2,184,370 | 2,184,370 | 2,184,578 | 2,184,578 |
| cel binary | 2,386,978 | 2,386,978 | 2,386,978 | 2,386,978 | 2,387,186 | 2,387,186 |
| python binary | 3,704,674 | 3,704,674 | 3,704,674 | 3,704,674 | 3,704,898 | 3,704,898 |
| duckdb binary | 6,464,882 | 6,464,882 | 6,464,882 | 6,448,370 | 6,448,578 | 6,448,578 |

- Table defaults shorten the source and leave the binary as it was: the table is static data.
- Direct bodies shrink the json binary by 16 KB, where the closures were generated code. Parsers
  with only typed bodies are unchanged.
- The constructors shrink the duckdb binary by 16 KB.
- The positional entries shrink the source of the large grammars by 1.1% to 1.3% and add 208 bytes
  of generic instantiation to the binaries.
- The chunk helpers save their fixed 1.3 KB; the call sites are slightly longer (`rt.Alloc(` for
  `astNew(`), so the net is 0.4% to 1.3% of the source for the small grammars and 0.02% for duckdb.

Start-up (the time of a test binary that runs no test, 100 runs rotated three times) does not move
with the table: duckdb 16.9, 17.0 and 21.4 ms before and 16.3, 17.0 and 22.1 ms after the positional
entries (the third round of each is a noisier machine state).

Parse speed, medians of six rotated rounds over 45 Go benchmarks (the generated parsers, typed and
untyped, the recognizers, and the benchmarks of the json, csv, cel, python and duckdb modules).
The ratio is the geometric mean of the median of the later step over that of the earlier one;
"separated" counts the benchmarks whose ranges over the rounds do not overlap.

| Step | Geomean | Separated |
| --- | --- | --- |
| Direct bodies against table defaults | 0.992 | 3, all faster (Arith_LeftRec parse 0.975, Arith_LeftRec recognize 0.957, Recovery recognize 0.984) |
| Constructors against direct bodies | 0.997 | none |
| Positional table against constructors | 0.999 | 1 (CSV recognize, 1.018, which the next step reverses) |
| Chunk helpers against positional table | 1.001 | 1 (CSV recognize, 0.976) |
| Chunk helpers against table defaults | 0.989 | 10, all faster (0.957 to 0.986) |

No benchmark is slower over the whole stack. The closure per rule dispatch was measurable on the
left-recursive grammar, whose growth calls a body repeatedly.

## Not Done

Four further repeated forms were measured and left alone, since each is below the threshold at
which the generated code would be meaningfully shorter:

- The `rt.` qualifier: 5,193 occurrences in duckdb (15 KB, 1.0%); aliases for the five most common
  runtime types would save 0.5% and make the generated signatures less direct to read.
- The per-rule comments `// name (Node body inlined)` and the like do not reach the generated
  parsers: the generator keeps one `// rule name` line per rule (603 lines, 13 KB, 0.8% of duckdb),
  which names the grammar rule for navigation, and that is the form to keep.
- Forwarding functions that only call one target do not occur: the generator does not emit a
  wrapper that no call needs, and no function of the four parsers has a body of one call.
- The level argument of `p.Call(rules[i], 0)`: 114 sites in duckdb, 3 bytes each (0.02%).
