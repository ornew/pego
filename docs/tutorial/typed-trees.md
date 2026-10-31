# Building Typed Trees

The configuration grammar of the previous lesson produces a tree that mirrors the text. A program that uses the
configuration wants a tree shaped like the data. This lesson declares types in the grammar, builds them with actions and
reads them from Go.

## What You Will Build

A `config.zp` grammar whose result is a `Config` with a list of `Entry` values, and a Go program that prints them.

## Before You Start

Complete [Parsing Structured Input](structured-input.md): you need `example.conf` from it.

## Declaring Types and Actions

A `Config` has a list of `Entry` values, each with a key and a typed value. In ZeroParse you declare these types in the
grammar and construct them with actions. Save as `config.zp`:

```zp
package config

type Key = node[string]
type Number = node[string]
type String = node[string]
type Bool = node[string]
type Value ~ Number | String | Bool

type Entry = {
    Key   Key
    Value Value
}

type Config = {
    Entries []Entry
}

@start
rule main Config = -ws es:entry* $$ => Config{Entries: $es.Value}

rule entry Entry = k:key -ws -"=" -ws v:value -ws => Entry{Key: $k, Value: $v}

rule value Value = number / string / bool
rule key Key = ([a-z_]+) #atomic
rule number Number = ([0-9]+) #atomic
rule string String = ("\"" [^"]* "\"") #atomic
rule bool Bool = ("true" / "false") #atomic

rule ws = (blank / comment)*
rule blank = [ \t\r\n]+
rule comment = "#" [^\n]*
```

```bash
zp parse -g config.zp -f sexpr < example.conf
```

```text
Config{Entries: [Entry{Key: Key("host"), Value: String("\"localhost\"")} Entry{Key: Key("port"), Value: Number("8080")} Entry{Key: Key("debug"), Value: Bool("true")}]}
```

What is new:

- `package config` is optional; it names the grammar's package and is used by the code generator
  (see [Shipping a Parser](shipping.md)). It must be the first item in the file.
- `type Key = node[string]` declares a **named node type** whose value is a string, for the leaves of the tree. A rule
  of that type matches the text with `(...) #atomic` and has no action: its node holds the text it matched. The text of a
  `String` includes its quotes, since it is the text the rule matched.
  The S-expression keeps the type name in front of the text: `Key("host")`, where a plain `node[string]` prints just
  `"host"`.
- `type Value ~ Number | String | Bool` is an alias of a **join type**: a value of any one of the members, much like a Go
  interface. `~` declares an alias, another name for a type; `=` defines a new struct type. Because `value` is declared as `Value`, the compiler checks that each alternative is one of the members.
- `type Entry = { ... }` defines a **struct type**; fields are separated by newlines or semicolons. Field types can be
  lists (`[]Entry`), optional values (`*T`), or joins.
- `rule name [Type] = expression` states the optional type of a rule. You can omit it and ZeroParse infers the type from the body, but
  declaring the types of the rules that build your tree documents them and keeps mistakes close to where they happen.
- `es:entry*` captures the whole list of entries. `$es` is a node, so `$es.Value` is its value, the list that
  `Config{Entries: $es.Value}` stores in the `Config` struct. Every node has the members `.Start`, `.End` and `.Value`.

The compiler rejects ill-typed grammars with a position in the grammar file. In a copy of `config.zp`, swapping the
arguments in the `entry` action:

```bash
zp parse -g bad-type.zp -f sexpr -i 'a=1'   # ... Entry{Key: $v, Value: $k}
```

```text
zp: bad-type.zp:21:60: cannot use Number | String | Bool as Key in field Key of Entry
bad-type.zp:21:69: cannot use Key as Number | String | Bool in field Value of Entry
```

and misspelling a field name:

```bash
zp parse -g bad-field.zp -f sexpr -i 'a=1'   # ... Entry{Key: $k, Valu: $v}
```

```text
zp: bad-field.zp:21:69: Entry has no field Valu
```

### Using the Tree From Go

The typed tree is what your program consumes. Put `config.zp` and an `example.conf` next to this `main.go`, which
embeds the grammar and prints the entries:

```go
package main

import (
	_ "embed"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/ornew/zeroparse"
)

//go:embed config.zp
var grammarSource string

func main() {
	p, err := zeroparse.CompileSource(grammarSource, "main")
	if err != nil {
		log.Fatal(err) // a mistake in the grammar
	}

	input, err := os.ReadFile(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}

	cfg, err := p.Parse(string(input))
	if err != nil {
		var se *zeroparse.SyntaxError
		if errors.As(err, &se) {
			fmt.Printf("line %d, column %d: %s\n", se.Line, se.Col, se.Message())
			fmt.Println("expected:", se.Expected)
		}
		log.Fatal(err)
	}

	entries := cfg.Field("Entries").(*zeroparse.Node)
	for _, e := range entries.Elems() {
		key := e.Field("Key").(*zeroparse.Node)
		value := e.Field("Value").(*zeroparse.Node)
		fmt.Printf("%-6s %-7s %s\n", key.Text(), value.Type(), value.Text())
	}
}
```

```bash
go run . example.conf
```

```text
host   String  "localhost"
port   Number  8080
debug  Bool    true
```

`node.Field(name)` returns the value of a struct field as an `any`: a `*zeroparse.Node` for node values, lists, tuples and
structs, or an `int`, `string` or `bool`. A list is a node whose elements `Len()`, `Elem(i)` and `Elems()` give.
A string node keeps the text it matched, which `Text()` returns. `value.Type()` tells you which member of the `Value` join
you got, so a `switch` on it is how you walk a join.

## Next Steps

[Parsing Expressions](expressions.md) builds a calculator. The guides [Declaring Types](../guide/types.md) and
[Actions](../guide/actions.md) cover struct and join types, the built-in functions and positions, and
[Reading Trees](../guide/reading-trees.md) covers reading a tree in Go.
