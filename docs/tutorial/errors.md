# Reporting Errors

Input is often wrong, and a parser is judged by what it says then. This lesson reads the error messages PEGO produces,
makes them friendlier and recovers from errors so that one run reports all of them.

## What You Will Build

A configuration parser that reports readable messages and keeps going after a bad line.

## Before You Start

Complete [Building Typed Trees](typed-trees.md): this lesson uses `config.pego` and `example.conf` from it.

## Reading the Messages

When the input does not match, PEGO reports the **farthest position** it reached and **what it expected there**. You have
already seen the format: `line:column: syntax error: expected a, b, c`, with names such as `end of input` first, then literals, then classes, and at most ten items. Errors in
`config.pego`:

```bash
printf 'host = "localhost"\nport == 8080\n' | pego parse -g config.pego -f sexpr
```

```text
pego: 2:7: syntax error: expected "#", "\"", "false", "true", [ \t\r\n], [0-9]
```

Line 2, column 7 is the second `=`: after `port =` the grammar wants a value (or whitespace, or a comment). An
unterminated string is reported at the end of the input:

```bash
pego parse -g config.pego -f sexpr -i 'name = "oops'
```

```text
pego: 1:13: syntax error: expected "\"", [^"]
```

Two things to remember about these messages:

- The position is the farthest point *any* alternative reached, which is usually where the real mistake is, but not
  always: a failed literal is reported at the place where the literal began. For the input `host = "localhost"` followed by the line `debug = tru`, the
  error is at 2:9, the `t` of `tru`, not at the end of the line.
- The list is mechanical (it is built from the literals and classes that failed there). To give users something
  friendlier, attach `@error(message="...")` to an expression. Replacing `value` in `config.pego` with

  ```pego
  rule value Value = (number / string / bool) @error(message="expected a number, a string or true/false")
  ```

  turns the same errors into

  ```text
  pego: 2:7: expected a number, a string or true/false
  ```

  (and `pego: 2:9: expected a number, a string or true/false` for `debug = tru`).

## Recovering From Errors

A parser for an editor or a compiler should report every mistake in one pass and still return a tree.
`#recover(skip=...)` on an expression says what to do when it fails: record the error, skip the input that `skip` matches
and continue. Here a bad entry skips the rest of its line. Save as `robust.pego`, which is `config.pego` with the entry
wrapped in `#recover`, and with the message of the previous section on `value`:

```pego
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

rule entry Entry = (k:key -ws -"=" -ws v:value -ws) #recover(skip=[^\n]* "\n"?) -ws => Entry{Key: $k, Value: $v}

rule value Value = (number / string / bool) @error(message="expected a number, a string or true/false")
rule key Key = ([a-z_]+) #atomic
rule number Number = ([0-9]+) #atomic
rule string String = ("\"" [^"]* "\"") #atomic
rule bool Bool = ("true" / "false") #atomic

rule ws = (blank / comment)*
rule blank = [ \t\r\n]+
rule comment = "#" [^\n]*
```

```bash
printf 'host = "localhost"\nport == 8080\ndebug = tru\n' | pego parse -g robust.pego -f sexpr
```

```text
Config{Entries: [Entry{Key: Key("host"), Value: String("\"localhost\"")} Entry{Key: nil, Value: nil} Entry{Key: nil, Value: nil}]}
```

The tree contains the good entry and one placeholder per skipped entry (their fields are `nil`). The errors go to
standard error with exit status 1:

```text
pego: 2:7: expected a number, a string or true/false
3:9: expected a number, a string or true/false
```

From Go, a recovered parse returns the tree together with an error of type `pego.SyntaxErrors`, and a parse that could
not recover returns a `*pego.SyntaxError`. Tell them apart with `errors.As`, as this program, which reads `robust.pego`
from the same directory, does:

```go
package main

import (
	_ "embed"
	"errors"
	"fmt"
	"log"

	"github.com/ornew/pego"
)

//go:embed robust.pego
var grammarSource string

func main() {
	p, err := pego.CompileSource(grammarSource, "main")
	if err != nil {
		log.Fatal(err)
	}

	input := "host = \"localhost\"\nport == 8080\ndebug = tru\n"
	cfg, err := p.Parse(input)

	var recovered pego.SyntaxErrors
	var fatal *pego.SyntaxError
	switch {
	case err == nil:
		fmt.Println("no errors")
	case errors.As(err, &recovered):
		// A tree and the errors that were skipped over.
		for _, e := range recovered {
			fmt.Printf("%d:%d: %s\n", e.Line, e.Col, e.Message())
		}
	case errors.As(err, &fatal):
		log.Fatalf("%d:%d: %s", fatal.Line, fatal.Col, fatal.Message())
	default:
		log.Fatal(err)
	}

	for _, e := range cfg.Field("Entries").(*pego.Node).Elems() {
		if e.Field("Key") == nil {
			continue // an entry that was skipped
		}
		fmt.Println("entry:", e.Field("Key").(*pego.Node).Text())
	}
}
```

```text
2:7: expected a number, a string or true/false
3:9: expected a number, a string or true/false
entry: host
```

Mistakes in the *grammar* are reported before any input is parsed, also with a position. For example, this
`bad.pego` has an undefined rule:

```pego
// bad.pego
@start
rule main = item
```

```text
pego: bad.pego:3:13: undefined rule item
```

Asking for a start rule that does not exist with `pego parse -g config.pego -s nosuch -i a` fails the same way:

```text
pego: config.pego: rule nosuch is not defined
```

## Next Steps

[Parsing Context-Sensitive Input](context.md) parses input that depends on what came before. The guide
[Error Messages](../guide/error-messages.md) and [Recovering from Errors](../guide/recovery.md) cover labels, cuts, recovery and the error types in depth, and the
[cookbook](../cookbook/README.md) has recipes for [error carets](../cookbook/error-carets.md) and [reporting several
errors](../cookbook/several-errors.md).
