# Parse indentation-based input

**Problem.** A file nests its entries by indentation, as YAML or Python do, and you want the nesting as Go maps. A line
that is indented inconsistently should be reported as that.

PEG has no memory, so the grammar keeps the indentation of the current block in a variable and checks it with
predicates.

```pego
// nested.pego
type Entry struct {
    Key      Match
    Value    *Match
    Children []Entry
}

def main = [indent = 0] es:block blank* $$ -> $es

// The entries at the current indentation.
def block = (blank* e:entry)+ -> map($1, (x) => $x.e)

// A line indented exactly at the current depth, with the lines below it that are indented deeper.
def entry: Entry = s:spaces [len($s) == indent] #error(message="inconsistent indentation")
    k:key ":" " "* v:value? eol cs:children?
    -> new Entry{Key: $k, Value: $v, Children: concat($cs)}

// If the next line is indented deeper, read it and its siblings with that depth as the new indentation.
// A variable is visible only in the rule that defines it and the rules it calls, so when this rule returns, the
// parent's indentation is back.
def children = &(blank* s:spaces) [len($s) > indent] [indent = len($s)] b:block -> $b

def spaces = @" "*
def key = @(?a-z_)+
def value = @(?^\n)+
def eol = "\n" / $$
def blank = (? \t)* "\n"
```

```go
// main.go
package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/ornew/pego"
)

//go:embed nested.pego
var grammar string

// toMap turns entries into nested maps: an entry with children becomes a map, any other a string.
func toMap(entries []*pego.Node) map[string]any {
	m := map[string]any{}
	for _, e := range entries {
		key := e.Field("Key").(*pego.Node).Text
		if kids := e.Field("Children").(*pego.Node).Children; len(kids) > 0 {
			m[key] = toMap(kids)
		} else if v, ok := e.Field("Value").(*pego.Node); ok {
			m[key] = v.Text
		} else {
			m[key] = ""
		}
	}
	return m
}

func main() {
	log.SetFlags(0) // no time stamps in front of the errors
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		log.Fatal(err)
	}
	src, err := os.ReadFile(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	tree, err := p.Parse(string(src))
	if err != nil {
		log.Fatal(err)
	}
	out, _ := json.MarshalIndent(toMap(tree.Children), "", "  ")
	fmt.Println(string(out))
}
```

With this `app.yml`:

```text
server:
  host: localhost
  port: 8080
  tls:
    cert: server.pem
    key: server.key

log: debug
```

run it:

```bash
go run . app.yml
```

```json
{
  "log": "debug",
  "server": {
    "host": "localhost",
    "port": "8080",
    "tls": {
      "cert": "server.pem",
      "key": "server.key"
    }
  }
}
```

A line that is indented to a depth that no enclosing block has, in `bad.yml`:

```text
server:
    host: localhost
  port: 8080
```

```bash
go run . bad.yml
```

```text
3:3: inconsistent indentation
```

## How it works

- **`[indent = 0]`** defines the variable at the start. A predicate `[name = value]` defines a variable; it never
  changes one. The definition is visible to the rules called after it, and ends when the rule that made it returns.
- **`entry`** matches the leading spaces, captures them as `s`, and the predicate `[len($s) == indent]` succeeds only
  at the current depth. This is also how a block ends: at a line with another indentation, `entry` fails and `block`'s
  repetition stops.
- **`children`** looks ahead at the next line with `&(blank* s:spaces)`. A capture made in a positive lookahead stays
  visible after it, so the predicates can read `$s` without consuming the spaces. If the line is indented deeper, the new
  depth is defined with `[indent = len($s)]`, and the rule `block` called after it sees the new depth. When `children`
  returns, the parent's `indent` is back: the variable is scoped by the rule invocation, and a counter is a chain of
  new bindings, never a mutation.
- **`#error` on the predicate.** A predicate that fails records no expected token, so without the label the message for
  the third line above is `syntax error: expected " ", "\n", (? \t)`, which is true and unhelpful.
- **`*Match` for the optional value.** `v:value?` is a `Match` or nothing, which the struct field must say with `*Match`;
  in Go, `Field("Value")` is then a `*pego.Node` or `nil`, which `v, ok := ....(*pego.Node)` tells apart.

## Variations

- **Tabs.** This grammar counts only spaces: a tab at the start of a line is a syntax error. To allow tabs, make
  `spaces` match them and decide what `len` should count, or reject them with a message of their own.
- **Values with structure.** Make `value` a rule of its own (numbers, quoted strings, lists), as in
  [Read a configuration file into Go structs](config-to-structs.md).
- **Lists and other forms.** `- item` lines are one more alternative of `entry` at the same indentation.
- **Cost.** A rule that reads a variable is cached per value of the variable, so keep the number of values small (a
  depth, not a position); see the guide below.
- **Python and YAML.** Their grammars, [parsers/python](../../parsers/python/) and [parsers/yaml](../../parsers/yaml/),
  use the same technique at a larger scale.

See [Context-sensitive parsing](../guide/context-sensitive.md) for predicates, variables and lookahead, and
[examples/outline](../../examples/outline/) for the grammar this one is based on.
