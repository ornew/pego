# Report an error with its source line and a caret

**Problem.** When a user's file does not parse, you want a message like those of a compiler: the file, the line and
the column, a message that says what is wrong, the line of the source, and a caret under the place.

```text
app.conf:2:8: expected a number or a string
2 | name = ;
  |        ^
```

Two things make this: a grammar whose errors have readable messages (`#error`), and a few lines of Go that print the
position of the `*pego.SyntaxError`.

```pego
// settings.pego
def main = -ws setting* $$

def setting = key -ws "=" #error(message="expected '='") -ws value -ws ";" #error(message="expected ';'") -ws

// _|_ always fails, so its label is for input that is neither. A string that is not closed fails further on,
// and its own message wins.
def value = number / string / _|_ #error(message="expected a number or a string")

def key = (?a-z_)+
def number = (?0-9)+
def string = "\"" (?^"\n)* "\"" #error(message="unterminated string")

// A failure inside a lookahead is not reported, so whitespace never shows up in "expected ...".
def ws = (&(? \t\r\n) .)*
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

//go:embed settings.pego
var grammar string

// report formats a syntax error with the line it is on and a caret under the column.
func report(file, src string, err error) string {
	var se *pego.SyntaxError
	if !errors.As(err, &se) {
		return file + ": " + err.Error()
	}
	lines := strings.Split(src, "\n")
	text := ""
	if se.Line <= len(lines) { // the error can be at the end of the input, after the last line break
		text = strings.TrimSuffix(lines[se.Line-1], "\r")
	}
	// The column counts code points. Keep tabs, so that the caret lines up with the text above it.
	var indent strings.Builder
	runes := []rune(text)
	for i := 0; i < se.Col-1; i++ {
		if i < len(runes) && runes[i] == '\t' {
			indent.WriteByte('\t')
		} else {
			indent.WriteByte(' ')
		}
	}
	gutter := strconv.Itoa(se.Line)
	return fmt.Sprintf("%s:%d:%d: %s\n%s | %s\n%s | %s^",
		file, se.Line, se.Col, se.Message(), gutter, text, strings.Repeat(" ", len(gutter)), indent.String())
}

func main() {
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		log.Fatal(err)
	}
	for _, src := range []string{
		"port = 8080;\nname = ;\n",
		"port = 8080;\n\tname = \"api\n",
		"port = 8080;\nhost = \"x\"",
		"port = 8080;\nwidth 12;\n",
		"port = 80 80;\n",
	} {
		if _, err := p.Parse(src); err != nil {
			fmt.Println(report("app.conf", src, err))
			fmt.Println()
		}
	}
}
```

```text
app.conf:2:8: expected a number or a string
2 | name = ;
  |        ^

app.conf:2:13: unterminated string
2 | 	name = "api
  | 	           ^

app.conf:2:11: expected ';'
2 | host = "x"
  |           ^

app.conf:2:7: expected '='
2 | width 12;
  |       ^

app.conf:1:11: expected ';'
1 | port = 80 80;
  |           ^
```

(The second source line starts with a tab, which the caret line repeats.)

## How it works

- **`*pego.SyntaxError` has the position.** `Line` and `Col` are 1-based. `Message()` is the text without the
  position, and `Error()` is `line:col: message`. Other errors, such as a failed action, are not syntax errors and have
  no position, which is why `report` falls back to `err.Error()`.
- **`#error` makes the message.** Without it the message would be `syntax error: expected "=", ...`: the list of every
  token that could have come at the farthest position, which is exact, and written for people who know the
  grammar. A label replaces the list with a sentence, at the farthest position inside the expression it labels. Put the
  label on the smallest expression that tells the user what is missing: here the `=` and the `;`, and the value.
- **Why `value` ends with `_|_`.** An outer label replaces the labels of the expressions inside it, and a label on a
  whole choice would hide the message of the unterminated string. With `_|_ #error(...)` as the last alternative, the
  message is recorded at the start of the value, and a string that starts but does not end has failed further on, so
  its own message wins.
- **Whitespace is quiet.** `ws` consumes spaces inside a lookahead, which records no failures. With `(? \t\r\n)*` every
  list of expected tokens would contain a space, a tab and a line break.
- **Columns count code points** by default, so a caret under non-ASCII text lines up with the characters, not the
  bytes, but not with their display width (a wide character or a combining mark). With `pego.WithUnit(pego.Bytes)`,
  `Col` counts bytes instead. A tab is one column, so `report` copies the tabs of the source line into the caret line.
- **The end of the input.** An error after the last line break has a line number one past the last line, so `report`
  shows an empty line instead of indexing past the end.

## Variations

- **Colors, ranges, context lines**: all of it is string formatting; the error gives only the position of the farthest
  failure. To underline a range, you need the node that failed, which a recovered `Error` node has (see [Report several
  errors in one run](several-errors.md)).
- **Where the message points.** A missing `;` at the end of a line is reported at the first token that did not fit, which
  can be on the next line, because the rule for white space reads past line ends. [Errors and
  recovery](../guide/errors-and-recovery.md) explains this and how to place labels.
- **Find out why a grammar reports what it does** with `pego explain -g settings.pego < app.conf`; see
  [Debugging and profiling](../guide/debugging.md).
- **Editors**: [`pego lsp`](../guide/editor-support.md) shows the same errors as you type.
