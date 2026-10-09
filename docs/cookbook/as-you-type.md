# Reparse a document on every keystroke

**Problem.** An editor, a preview or a linter holds a text that the user edits, and wants a fresh tree and the current
errors after every keystroke, without parsing the whole text again each time.

Keep a `Document`: it holds the text and remembers what the last parse did. `Edit` records a change, and `Parse` runs
the grammar again, but takes every result that the change did not touch from the last parse.

The grammar is that of [Report several errors in one run](several-errors.md), with a root that builds nothing. A text that
is half typed is not valid, so the grammar recovers from errors, and every keystroke gets a tree and the errors so far.

```pego
// settings.pego
type Setting struct { Key Match, Value Match }

def main = -ws (setting -ws)* $$

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

The program builds a text of 10,000 settings, then types `timeout = 30;` on a new line in the middle of it, one
character at a time, and prints what each `Parse` cost.

```go
// main.go
package main

import (
	_ "embed"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ornew/pego"
)

//go:embed settings.pego
var grammar string

func main() {
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		log.Fatal(err)
	}

	// A file of 10,000 settings, one per line.
	var b strings.Builder
	for i := 0; i < 10000; i++ {
		fmt.Fprintf(&b, "setting_%c = %d;\n", 'a'+i%26, i)
	}
	text := b.String()

	doc, err := p.NewDocument(text)
	if err != nil {
		log.Fatal(err)
	}
	start := time.Now()
	doc.Parse()
	fmt.Printf("%-18s %8s  evaluated %5d  reused %5d\n", "first parse", time.Since(start).Round(time.Microsecond), doc.Stats().Evaluated, doc.Stats().Reused)

	// The user opens a new line in the middle of the file and types a setting, one character at a time.
	pos := strings.Index(text, "setting_a = 4992;")
	doc.Edit(pos, pos, "\n")
	for _, ch := range "timeout = 30;" {
		if err := doc.Edit(pos, pos, string(ch)); err != nil {
			log.Fatal(err)
		}
		pos++
		start := time.Now()
		_, err := doc.Parse() // an editor would show the errors and use the tree where it can
		elapsed := time.Since(start)
		var n int
		if errs, ok := err.(pego.SyntaxErrors); ok {
			n = len(errs)
		}
		fmt.Printf("typed %-12q %8s  evaluated %5d  reused %5d  errors %d\n",
			string(ch), elapsed.Round(time.Microsecond), doc.Stats().Evaluated, doc.Stats().Reused, n)
	}

	// For comparison: parsing the final text from scratch.
	start = time.Now()
	p.Parse(doc.Text())
	fmt.Printf("%-18s %8s\n", "parse from scratch", time.Since(start).Round(time.Microsecond))
}
```

On an Apple M3 Max with Go 1.27.1 (the times differ from run to run; `evaluated` and `reused` do not much):

```text
first parse         18.96ms  evaluated 90005  reused     0
typed "t"           2.374ms  evaluated     6  reused 20002  errors 1
typed "i"           1.823ms  evaluated     4  reused 20004  errors 1
typed "m"           2.599ms  evaluated     4  reused 20004  errors 1
typed "e"           1.369ms  evaluated     4  reused 20004  errors 1
typed "o"           1.178ms  evaluated     4  reused 20004  errors 1
typed "u"           1.348ms  evaluated     4  reused 20004  errors 1
typed "t"           1.798ms  evaluated     4  reused 20004  errors 1
typed " "            2.32ms  evaluated     5  reused 20003  errors 1
typed "="           2.791ms  evaluated     6  reused 20005  errors 1
typed " "           1.447ms  evaluated     4  reused 20006  errors 1
typed "3"           1.397ms  evaluated     6  reused 20006  errors 1
typed "0"           1.243ms  evaluated     5  reused 20007  errors 1
typed ";"           1.523ms  evaluated     6  reused 20006  errors 0
parse from scratch  5.243ms
```

## How it works

- **`Stats` shows what was reused.** `Evaluated` is the number of rule bodies that ran, `Reused` the number of results
  taken from the last parse. The first parse ran 90,005 rule bodies. After a keystroke, 4 to 6 ran: the root, the
  setting being typed and the rules inside it, and the 20,000 or so other results were reused. The first parse is slower
  than a plain `Parse` (19 ms against 5 ms here), because a `Document` memoizes every rule call so that an edit can reuse
  any of them.
- **Why only a few.** A result is kept if the edit is outside the text that its rule looked at. A setting looks at its own
  text and the character after it, so settings elsewhere are kept; the root and the rules that contain the edit look at
  it, so they run again. The repetition at the root, 10,000 settings long, is not run element by element: it resumes
  from the last parse. See the guide for the details.
- **The cost still grows with the document.** Taking 20,000 results is not free. For this grammar, a keystroke cost
  about 0.1 ms for 1,000 lines, 1.2 ms for 10,000 lines and 16 ms for 100,000 lines, in the median over the 13
  keystrokes, against about 0.45, 4.6 and 47 ms to parse from scratch (measured with a variant of the program that
  takes the number of lines as an argument). Reuse is about four times faster here, not independent of the size, and
  the grammar decides how much: see below.
- **Errors are part of the result.** A half-typed line is an error, and `Parse` returns the tree together with a
  `pego.SyntaxErrors`, as in the previous recipe. After `timeout = 30;` is complete, the errors are gone.
- **`Edit(start, end, text)` counts in the position unit of the document** (code points by default; `Bytes` with
  `pego.WithUnit(pego.Bytes)` when `NewDocument` is created). It only records the change; the next `Parse` does the work, and
  several edits can come before one.

## Writing a grammar that reuses well

The unit of reuse is the rule call, so:

- **Give each unit its own rule**: one line, one statement, one declaration, one list item, called from a flat
  repetition (`line*`). A grammar written as one big rule has little to reuse.
- **Use a repetition for a list, not recursion.** With `lines = line lines?`, every call before the edit looks at
  everything after it, so all of them run again.
- **Avoid lookahead that can reach the end of the text** and positions stored in values (`startPos` in an action); both
  make the rules that use them run again after every edit.
- **Check with `Stats`** as above: a good grammar evaluates a handful of rule bodies per keystroke.

## Variations

- **Keep the last good tree.** A tree returned by `Parse` is changed in place by the next `Parse` (nodes that are reused
  are moved to their new positions). To keep it, take `tree.Clone()` before editing again.
- **Concurrency.** A `Document` is not safe for concurrent use; a `Parser` is. Use one document per editor buffer.
- **Time limit per keystroke.** If a parse can take long, parse after the user pauses, and apply the edits made
  meanwhile together, with several `Edit` calls before one `Parse`.
- **Only the errors.** A `Document` cannot be used with `RecognizeOnly`; it always builds the tree.

See the guide on [incremental parsing](../guide/README.md) (in the index, under streaming and incremental parsing) for
the model of reuse, the full API, and more on writing grammars that reuse well.
