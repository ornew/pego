# Position Units

Every position that PEGO reports is a count in one of two units. This guide shows what the unit changes, how to choose
it and what invalid UTF-8 does.

The examples use `pairs.pego` and a parser `p` compiled as in [Running Parsers](running-parsers.md):

```pego
type Pair = { Key Match; Value Match }

rule main = ws ps:pair+ $$ => $ps
rule pair Pair = k:key "=" v:value ";"? ws => Pair{Key: $k, Value: $v}
rule key = [a-zあ-ん]+ #atomic
rule value = [0-9]+ #atomic
rule ws = [ \t\n]*
```

Positions can count **Unicode code points** (the default) or **UTF-8 bytes**. The unit affects node `Start` and `End`,
the `Pos` and `Col` of syntax errors, `startPos` and `endPos` in actions, the `len` builtin, and the ranges of
`Document.Edit`. It never changes what matches: matching always proceeds by code point.

```go
n, _ := p.Parse("あい=3")
fmt.Println(n.Children[0].Field("Key").(*pego.Node).End) // 2
n, _ = p.Parse("あい=3", pego.WithUnit(pego.Bytes))
fmt.Println(n.Children[0].Field("Key").(*pego.Node).End) // 6
```

Syntax errors follow the unit, too (the same `あい=x` input, with the error at the `x`):

```bash
$ pego parse -g pairs.pego -i 'あい=x'
pego: 1:4: syntax error: expected [0-9]
$ pego parse -g pairs.pego -unit bytes -i 'あい=x'
pego: 1:8: syntax error: expected [0-9]
```

Choosing:

- Use **bytes** when you index into the Go string (`input[n.Start:n.End]`), pass positions to other byte-oriented
  tools, or want to avoid converting offsets. Bytes is the natural unit for Go.
- Use **code points** (the default) when positions are shown to people as character counts, or are exchanged with a
  system that counts characters (editors and language servers often count UTF-16 units, which neither unit matches).
- Invalid UTF-8 is read as one U+FFFD character per invalid byte, so positions stay well defined; the decoding rules
  and their effect on `Match.Text` are in [Positions](../../spec/parsing.md#positions).

The [benchmarks](../benchmarks.md#parsing-time-position-unit-bytes) show almost no speed difference between the units.
The CLI flag is `-unit codepoints|bytes`.

## Related Topics

- [Running Parsers](running-parsers.md): compiling and calling a parser.
- [Reading Trees](reading-trees.md): reading `Start` and `End` of a node.
- [Positions](../../spec/parsing.md#positions): the definition of units and decoding.
