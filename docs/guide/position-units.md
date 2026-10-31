# Position Units

Every position that ZeroParse reports is a count in one of two units. This guide shows what the unit changes, how to choose
it and what invalid UTF-8 does.

The examples use `pairs.zp` and a parser `p` compiled as in [Running Parsers](running-parsers.md):

```zp
type Pair = { Key node[string]; Value node[string] }

@start
rule main = ws ps:pair+ $$ => $ps
rule pair Pair = k:key "=" v:value ";"? ws => Pair{Key: $k, Value: $v}
rule key = [a-zあ-ん]+ #atomic
rule value = [0-9]+ #atomic
rule ws = [ \t\n]*
```

Positions can count **Unicode code points** (the default) or **UTF-8 bytes**. The unit affects node `Start()` and `End()`,
the `Pos` and `Col` of syntax errors, `.Start` and `.End` in actions, the `len` builtin, and the ranges of
`Document.Edit`. It never changes what matches: matching always proceeds by code point.

```go
n, _ := p.Parse("あい=3")
fmt.Println(n.Elem(0).Field("Key").(*zp.Node).End()) // 2
n, _ = p.Parse("あい=3", zp.WithUnit(zp.Bytes))
fmt.Println(n.Elem(0).Field("Key").(*zp.Node).End()) // 6
```

Syntax errors follow the unit, too (the same `あい=x` input, with the error at the `x`):

```bash
zp parse -g pairs.zp -i 'あい=x'
```

```text
zp: 1:4: syntax error: expected [0-9]
```

```bash
zp parse -g pairs.zp -unit bytes -i 'あい=x'
```

```text
zp: 1:8: syntax error: expected [0-9]
```

Choosing:

- Use **bytes** when you index into the Go string (`input[n.Start():n.End()]`), pass positions to other byte-oriented
  tools, or want to avoid converting offsets. Bytes is the natural unit for Go.
- Use **code points** (the default) when positions are shown to people as character counts, or are exchanged with a
  system that counts characters (editors and language servers often count UTF-16 units, which neither unit matches).
- Invalid UTF-8 is read as one U+FFFD character per invalid byte, so positions stay well defined; the decoding rules
  and their effect on the text of string nodes are in [Positions](../../spec/runtime/parsing.md#positions).

The [benchmarks](../benchmarks.md#parsing-time-position-unit-bytes) show almost no speed difference between the units.
The CLI flag is `-unit codepoints|bytes`.

## Related Topics

- [Running Parsers](running-parsers.md): compiling and calling a parser.
- [Reading Trees](reading-trees.md): reading `Start` and `End` of a node.
- [Positions](../../spec/runtime/parsing.md#positions): the definition of units and decoding.
