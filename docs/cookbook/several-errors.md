# Report several errors in one run

**Problem.** A file has three mistakes, and you want the user to see all three, not to fix one, run again and find the
next. You also want the lines that were fine, so that the rest of the program can still use them.

Mark the unit that can go wrong (here a setting) with `#recover`. When it fails, the parser records the error, skips
to a place where parsing can go on, and puts an `Error` node where the setting would have been.

```pego
// settings.pego
type Setting struct { Key Match, Value Match }
type File struct { Settings []Setting }

def main: File = -ws ss:(s:setting -ws)* $$ -> new File{Settings: map($ss, (x) => $x.s)}

// If a setting does not parse, skip to the next ";" (but not past a line end) and carry on.
def setting: Setting = s:entry #recover(skip=(?^;\n)+ ";"?) -> $s

def entry: Setting = k:@key -ws "=" #error(message="expected '='") -ws v:value -ws ";" #error(message="expected ';'")
    -> new Setting{Key: $k, Value: $v}
def value = @number / @string / _|_ #error(message="expected a number or a string")

def key = (?a-z_)+
def number = (?0-9)+
def string = "\"" (?^"\n)* "\"" #error(message="unterminated string")

def ws = (&(? \t\r\n) .)*
```

The program reads this `app.conf`, in which three settings are wrong (`name` has no value, `width` has no `=`, and
`true` is not a value of this grammar):

```text
port = 8080;
name = ;
host = "x";
width 12;
debug = true;
timeout = 30;
```

```go
// main.go
package main

import (
	_ "embed"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/ornew/pego"
)

//go:embed settings.pego
var grammar string

func main() {
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		log.Fatal(err)
	}
	src, err := os.ReadFile("app.conf")
	if err != nil {
		log.Fatal(err)
	}

	tree, err := p.Parse(string(src))

	// With recovery, err is not nil even though there is a tree: SyntaxErrors holds every error that was recovered
	// from. Any other error means that there is no tree.
	var errs pego.SyntaxErrors
	if err != nil && !errors.As(err, &errs) {
		log.Fatal(err) // a *pego.SyntaxError: nothing could be recovered
	}
	for _, e := range errs {
		fmt.Printf("app.conf:%d:%d: %s\n", e.Line, e.Col, e.Message())
	}

	// The tree has the settings that parsed, and an Error node where one did not.
	for _, s := range tree.Field("Settings").(*pego.Node).Children {
		if s.Type() == "Error" {
			fmt.Printf("skipped %q\n", s.Text)
			continue
		}
		fmt.Printf("%s = %s\n", s.Field("Key").(*pego.Node).Text, s.Field("Value").(*pego.Node).Text)
	}
	if len(errs) > 0 {
		os.Exit(1)
	}
}
```

```text
app.conf:2:8: expected a number or a string
app.conf:4:7: expected '='
app.conf:5:9: expected a number or a string
port = 8080
skipped "name = ;"
host = "x"
skipped "width 12;"
skipped "debug = true;"
timeout = 30
```

The program exits with status 1, because there were errors.

## How it works

- **`#recover(skip=e)`** is attached to an expression like `#error`. If the expression fails, the error is recorded, the
  parser goes back to where the expression *started*, and matches `e` there. The input `e` matches is skipped, and the
  expression's value is an `Error` node over it. If `e` fails or matches nothing, there is no recovery, so a recovery in
  a repetition cannot loop forever.
- **`skip` starts at the start of the setting**, not where the error was found. It says "the rest of this setting": any
  characters up to a `;` or the end of the line, and the `;`. A setting that is missing its `;` is skipped to the end
  of its line, and the next setting is read from the next line. (The error is then reported on that next line, at
  the first token that did not fit: `name = 5` followed by `host = 1;` gives `3:1: expected ';'` in the file
  `port = 1;`, `name = 5`, `host = 1;`.)
- **Types.** `Error` is assignable to every node type, so the `Setting` rule and the `[]Setting` field can hold it. Go sees
  `s.Type() == "Error"`, with the skipped text in `s.Text` and the message, with its position (`2:8: expected ...`), in `s.Field("message")`.
- **`pego.SyntaxErrors` is the second kind of result.** `Parse` returns the tree and an error that is a
  `pego.SyntaxErrors` (a slice of `*pego.SyntaxError`, each with `Line`, `Col` and `Message()`). Do not write
  `if err != nil { return err }` first: that throws the tree away. `errors.As` needs a variable of the slice type, not a
  pointer.
- **The order of the attributes matters**: `#error(...) #recover(...)`, in that order, so that the recorded error carries
  the label. Here the labels are inside `entry`, which `setting` wraps with `#recover`.

## Limits

- **Recovery needs a skip that can match.** `skip` must match at least one character where the unit started. Take
  the file `port = 1;`, `;`, `name = 2;`: the second line is a setting that fails, and `skip=(?^;\n)+` cannot match
  at a `;`. There is no recovery, and `Parse` fails as usual with one error and no tree
  (`2:1: syntax error: expected (?^;\n), (?a-z_), end of input`). Think of what each `skip` does at every place where the
  unit can fail, and test it with inputs that are wrong in different ways (see [Test hand-written code against a
  grammar](test-with-samples.md) for generating them).
- **Unclosed constructs.** Errors recovered inside a block that is never closed are lost when the block fails at the
  end of the input, and only the missing closing token is reported. The [guide](../guide/errors-and-recovery.md)
  explains why.
- **The skip is a guess** about where the next unit begins. A `skip` that is too eager swallows good input, and one that
  stops too soon produces a second, bogus error.
- **Do not use the tree for code that must not run on broken input.** Check `len(errs)` before acting on it.

## Variations

- **Several error formats.** Show each error with a caret, using `report` of [Report an error with its source line and a
  caret](error-carets.md) on each element of `errs`.
- **Recovery inside a block.** Put `#recover` on the statement rule of the block body, with a `skip` that does not eat the
  closing brace; [Errors and recovery](../guide/errors-and-recovery.md) has the recipe.
- **Streams.** `ParseStream` reports recovered errors in the same way; see [Process a file that does not fit in
  memory](big-files.md).
- **Editors.** A language server needs exactly this: errors with positions and a tree of what could be read.

See [spec/attributes.md](../../spec/attributes.md) for the exact rules of `#error` and `#recover`.
