# Read a configuration file into Go structs

**Problem.** Your program reads a small configuration file of `key = value` lines, with comments, strings, integers,
booleans and arrays, and wants a Go struct. A wrong type or an unknown key should be reported with its line.

The grammar describes the syntax and keeps each kind of value as a node type. Go turns the nodes into a struct.

```pego
// config.pego
package config

type Key terminal
type Int terminal
type Str terminal
type Bool terminal
type Array struct { Items []Value }
type Value = Int | Str | Bool | Array
type Entry struct { Key Key, Value Value }
type Config struct { Entries []Entry }

def main: Config = -ws es:entry* $$ -> new Config{Entries: $es}

def entry: Entry = k:key -ws -"=" -ws v:value -ws -> new Entry{Key: $k, Value: $v}

def value: Value = int / str / bool / array
def key: Key = (?a-z_)+
def int: Int = "-"? (?0-9)+
def str: Str = "\"" ("\\" . / (?^"\\\n))* "\""
def bool: Bool = "true" / "false"
def array: Array = "[" -ws vs:(v:value -ws -","? -ws)* "]" -> new Array{Items: map($vs, (x) => $x.v)}

def ws = (blank / comment)*
def blank = (? \t\r\n)+
def comment = "#" (?^\n)*
```

```go
// main.go
package main

import (
	_ "embed"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/ornew/pego"
)

//go:embed config.pego
var grammar string

// Config is what the program wants from the file.
type Config struct {
	Name  string
	Port  int
	Debug bool
	Tags  []string
}

func asString(v *pego.Node) (string, error) {
	if v.Type() != "Str" {
		return "", fmt.Errorf("want a string, got %s", strings.ToLower(v.Type()))
	}
	return strconv.Unquote(v.Text)
}

func asInt(v *pego.Node) (int, error) {
	if v.Type() != "Int" {
		return 0, fmt.Errorf("want an integer, got %s", strings.ToLower(v.Type()))
	}
	return strconv.Atoi(v.Text)
}

func asBool(v *pego.Node) (bool, error) {
	if v.Type() != "Bool" {
		return false, fmt.Errorf("want true or false, got %s", strings.ToLower(v.Type()))
	}
	return v.Text == "true", nil
}

func asStrings(v *pego.Node) ([]string, error) {
	if v.Type() != "Array" {
		return nil, fmt.Errorf("want an array, got %s", strings.ToLower(v.Type()))
	}
	var out []string
	for _, item := range v.Field("Items").(*pego.Node).Children {
		s, err := asString(item)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// Load parses src and copies the entries into a Config.
func Load(p *pego.Parser, src string) (Config, error) {
	var c Config
	// Bytes, so that Start is an offset into src.
	tree, err := p.Parse(src, pego.WithUnit(pego.Bytes))
	if err != nil {
		return c, err
	}
	for _, e := range tree.Field("Entries").(*pego.Node).Children {
		key := e.Field("Key").(*pego.Node).Text
		v := e.Field("Value").(*pego.Node)
		switch key {
		case "name":
			c.Name, err = asString(v)
		case "port":
			c.Port, err = asInt(v)
		case "debug":
			c.Debug, err = asBool(v)
		case "tags":
			c.Tags, err = asStrings(v)
		default:
			err = errors.New("unknown key")
		}
		if err != nil {
			line := 1 + strings.Count(src[:e.Start], "\n")
			return c, fmt.Errorf("line %d: %s: %w", line, key, err)
		}
	}
	return c, nil
}

func main() {
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		log.Fatal(err)
	}
	for _, src := range []string{
		"# server settings\nname = \"api\"\nport = 8080\ndebug = true\ntags = [\"a\", \"b\\tc\"]\n",
		"name = \"api\"\nport = \"8080\"\n",
		"port = 80\nhost = \"x\"\n",
		"port = 80\ndebug = yes\n",
	} {
		c, err := Load(p, src)
		if err != nil {
			fmt.Println("error:", err)
			continue
		}
		fmt.Printf("%+v\n", c)
	}
}
```

```text
{Name:api Port:8080 Debug:true Tags:[a b	c]}
error: line 2: port: want an integer, got str
error: line 2: host: unknown key
error: 2:9: syntax error: expected "#", "-", "[", "\"", "false", "true", (? \t\r\n), (?0-9)
```

(`b\tc` in the first input is a tab, which `strconv.Unquote` decoded; the output shows it as a tab.)

## How it works

- **Syntax in the grammar, meaning in Go.** The grammar says what a file looks like and tells the kinds of value apart
  by type: `Int`, `Str`, `Bool` and `Array` are different node types, so Go can switch on `v.Type()`. Actions have no
  function that turns text into a number, so `strconv` does that in the `as...` helpers.
- **The tree is what the grammar declares.** `main` returns a `Config` node whose `Entries` field is a list of `Entry`
  nodes. `Field("Entries")` and `Children` walk it, as in the [getting-started
  tutorial](../tutorial/getting-started.md#5-typed-trees).
- **`pego.WithUnit(pego.Bytes)`** makes `Start` and `End` offsets into the Go string, so `src[:e.Start]` is the text
  before an entry, and counting its line feeds gives the line. By default positions count code points, which are not
  string indexes when the text has non-ASCII characters.
- **Two kinds of error.** A file that is not in the format fails in `Parse` with a `*pego.SyntaxError` (the last line of
  the output). A file that has the format but not the meaning your program needs, such as a wrong type or an unknown
  key, fails in your code, which knows the position of the entry.

## Variations

- The syntax error lists every token that could have come next. [Report an error with its source line and a
  caret](error-carets.md) shows how to replace it with a message such as `expected a value`.
- To get typed Go values without writing the `Field` and `Children` code, generate a parser with `pego gen -types`; see
  [Ship a parser and keep it up to date](ship-a-parser.md).
- Sections (`[server]`), a trailing comma rule or `key: value` lines are changes to `entry` and `main`. Keep every rule
  that skips white space in one place (`ws` here), so that the grammar says where white space may appear.
- If the file is YAML, JSON, CSV or XML, use a [ready-made parser](ready-made-parsers.md) instead of a grammar of your
  own.

See [Trees and actions](../guide/trees-and-actions.md) for shaping the tree and [Running
parsers](../guide/runtime.md) for the Go API.
