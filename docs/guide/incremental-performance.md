# Incremental Parsing Performance

How much an edit costs depends on the grammar and on where the edit is. This guide gives measurements of reparsing after
a one-character edit, and the limits of the engine `Document`. It assumes the `Document` API of [Incremental
Parsing](incremental.md).

## Performance and Limits

Measured with this program (Apple silicon, Go 1.27.1, averages over repeated runs; it edits one digit in the middle of
the text and checks that the incremental tree equals a fresh parse):

<details>
<summary>The benchmark program</summary>

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"strings"
	"time"

	"github.com/ornew/pego"
)

// Three grammars for a file of "key = value" lines.
var grammars = []struct {
	name, grammar string
	line          func(i int) string
}{
	{"settings, one rule per line", `rule main =  (setting / blank)* $$
rule setting =  key " = " value "\n"
rule key =  [a-z_]+ #atomic
rule value =  [^\n]+ #atomic
rule blank =  "\n" `,
		func(i int) string { return fmt.Sprintf("key_%c = value %d\n", 'a'+i%26, i) }},

	{"calc (Operators Rule expressions)", `
rule main =  (assign / blank)* $$
rule assign =  n:name " = " e:expr "\n"
rule name =  [a-z_]+ #atomic
rule blank =  "\n"
rule expr = operators {
    operand [0-9]+ #atomic
    operand "(" x:expr ")" => $x
    level { infix left "+" / "-" }
    level { infix left "*" / "/" }
}`,
		func(i int) string { return fmt.Sprintf("x_%c = 1+2*(3-%d)*4/5+6\n", 'a'+i%26, i%10) }},

	{"settings, everything in one rule", `rule main =  ([a-z_]+ #atomic " = " [^\n]+ #atomic "\n" / "\n")* $$ `,
		func(i int) string { return fmt.Sprintf("key_%c = value %d\n", 'a'+i%26, i) }},
}

// avg returns the average duration of n runs of f.
func avg(n int, f func()) time.Duration {
	start := time.Now()
	for i := 0; i < n; i++ {
		f()
	}
	return time.Since(start) / time.Duration(n)
}

// live returns how many MiB of heap are still reachable after build has run and the garbage was collected.
func live(build func() any) float64 {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	keep := build()
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(keep)
	return float64(after.HeapAlloc-before.HeapAlloc) / (1 << 20)
}

func main() {
	for _, g := range grammars {
		p, err := pego.CompileSource(g.grammar, "main")
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(g.name)
		for _, lines := range []int{1000, 10000, 100000} {
			var b strings.Builder
			for i := 0; i < lines; i++ {
				b.WriteString(g.line(i))
			}
			text := b.String()
			runs := 200000 / lines

			parse := avg(runs, func() { p.Parse(text) })
			first := avg(runs, func() {
				doc, _ := p.NewDocument(text)
				doc.Parse()
			})

			// Replace the first digit after "= " in the middle of the text, over and over.
			doc, _ := p.NewDocument(text)
			doc.Parse()
			pos := strings.Index(text[len(text)/2:], "= ") + len(text)/2 + 2 // the text is ASCII
			var edit, reparse time.Duration
			for i := 0; i < runs; i++ {
				start := time.Now()
				doc.Edit(pos, pos+1, string(rune('0'+i%10)))
				edit += time.Since(start)
				start = time.Now()
				doc.Parse()
				reparse += time.Since(start)
			}
			st := doc.Stats()

			// The incremental result must equal a fresh parse.
			got, _ := doc.Parse()
			want, _ := p.Parse(doc.Text())
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(want)

			fmt.Printf("  %6d lines: Parse %-8v NewDocument+Parse %-8v Edit %-8v reparse %-8v (evaluated %d, reused %d) equal=%v\n",
				lines, parse.Round(10*time.Microsecond), first.Round(10*time.Microsecond),
				(edit / time.Duration(runs)).Round(10*time.Microsecond), (reparse / time.Duration(runs)).Round(10*time.Microsecond),
				st.Evaluated, st.Reused, string(gotJSON) == string(wantJSON))
		}
		if g.name == grammars[0].name {
			var b strings.Builder
			for i := 0; i < 100000; i++ {
				b.WriteString(g.line(i))
			}
			text := b.String()
			tree := live(func() any { n, _ := p.Parse(text); return n })
			both := live(func() any {
				doc, _ := p.NewDocument(text)
				n, _ := doc.Parse()
				return []any{doc, n}
			})
			fmt.Printf("  memory at 100000 lines (%.1f MiB of text): tree from Parse %.0f MiB, Document with its tree %.0f MiB\n",
				float64(len(text))/(1<<20), tree, both)
		}
	}
}
```

</details>

```
settings, one rule per line
    1000 lines: Parse 230µs    NewDocument+Parse 580µs    Edit 0s       reparse 20µs     (evaluated 3, reused 1001) equal=true
   10000 lines: Parse 2.47ms   NewDocument+Parse 5.21ms   Edit 30µs     reparse 70µs     (evaluated 3, reused 10001) equal=true
  100000 lines: Parse 25.06ms  NewDocument+Parse 41.75ms  Edit 330µs    reparse 610µs    (evaluated 3, reused 100001) equal=true
  memory at 100000 lines (1.9 MiB of text): tree from Parse 27 MiB, Document with its tree 151 MiB
calc (Operators Rule expressions)
    1000 lines: Parse 1.8ms    NewDocument+Parse 2.24ms   Edit 10µs     reparse 30µs     (evaluated 3, reused 1002) equal=true
   10000 lines: Parse 15.55ms  NewDocument+Parse 18.74ms  Edit 40µs     reparse 190µs    (evaluated 3, reused 10002) equal=true
  100000 lines: Parse 158.72ms NewDocument+Parse 169.44ms Edit 360µs    reparse 1.85ms   (evaluated 3, reused 100002) equal=true
settings, everything in one rule
    1000 lines: Parse 170µs    NewDocument+Parse 180µs    Edit 0s       reparse 10µs     (evaluated 1, reused 999) equal=true
   10000 lines: Parse 1.74ms   NewDocument+Parse 2.51ms   Edit 40µs     reparse 100µs    (evaluated 1, reused 9999) equal=true
  100000 lines: Parse 19.91ms  NewDocument+Parse 25.23ms  Edit 360µs    reparse 620µs    (evaluated 1, reused 99999) equal=true
```

Reading the table:

- **Neither step grows much with the document.** The program replaces one digit, so nothing after the edit moves:
  the reparse takes the lines before and after the edit from the resumed `line*` and parses one line, about 0.6 ms at
  100,000 lines. An edit that changes the length also moves the nodes of every reused line after it in place, which is
  linear in the size of the document. `Edit` splices the text and its offset table, and in the memo table touches only
  the entries at the edited positions (the others are brought up to date when they are next looked up): about 0.3 ms
  at 100,000 lines, most of it copying the text.
- **The gain depends on how much a reused unit costs.** With the Operators Rule-expression lines, `Edit` and the reparse together
  take about 2 ms against 159 ms for a fresh parse at 100,000 lines. With the cheap `key = value` lines, about 1 ms
  against 25 ms. Everything in one rule gains as much: its repetition resumes.
- **The first parse is slower than `Parse`** (up to about 1.5 times with cheap rules, close to equal otherwise), because
  nearly every rule call is memoized (an ordinary parse memoizes only calls that repeat).
- **Memory is a multiple of the tree.** The line starting "memory" shows the live heap for the first grammar at 100,000
  lines: the tree from `Parse` alone against a `Document` that also holds the memo table (an entry per rule call), the
  records of long repetitions (about 80 bytes per element), the text as code points (four bytes each) and an offset
  table next to the string.
- **Bursts of edits are fine.** An `Edit` costs little on its own, and entries that several edits affect are brought
  up to date once, when they are looked up. Merging adjacent changes into one `Edit` still saves copying the text.

A real grammar, [parsers/json](../../parsers/json/json.pego), on an array of 5,000 objects (283 KB). Run it from the
root of the repository:

<details>
<summary>The program</summary>

```go
package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/ornew/pego"
)

// Run it from the root of the repository: go run ./jsondoc parsers/json/json.pego
func main() {
	src, err := os.ReadFile(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	p, err := pego.CompileSource(string(src), "main")
	if err != nil {
		log.Fatal(err)
	}

	// An array of 5,000 objects.
	var b strings.Builder
	b.WriteString("[\n")
	for i := 0; i < 5000; i++ {
		if i > 0 {
			b.WriteString(",\n")
		}
		fmt.Fprintf(&b, `  {"id": %d, "name": "item %d", "tags": ["a", "b"]}`, i, i)
	}
	b.WriteString("\n]\n")
	text := b.String()
	fmt.Printf("%d bytes\n", len(text))

	start := time.Now()
	if _, err := p.Parse(text); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Parse:               ", time.Since(start).Round(time.Millisecond))

	doc, err := p.NewDocument(text)
	if err != nil {
		log.Fatal(err)
	}
	start = time.Now()
	if _, err := doc.Parse(); err != nil {
		log.Fatal(err)
	}
	fmt.Println("first Document.Parse:", time.Since(start).Round(time.Millisecond), doc.Stats())

	// Change the last digit of "id": 2500.
	pos := strings.Index(text, `"id": 2500`) + len(`"id": 250`) // the text is ASCII
	start = time.Now()
	if err := doc.Edit(pos, pos+1, "7"); err != nil {
		log.Fatal(err)
	}
	edit := time.Since(start)
	start = time.Now()
	if _, err := doc.Parse(); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Edit:                ", edit.Round(10*time.Microsecond))
	fmt.Println("reparse:             ", time.Since(start).Round(10*time.Microsecond), doc.Stats())
}
```

</details>

```
$ go run ./jsondoc parsers/json/json.pego
282783 bytes
Parse:                16ms
first Document.Parse: 31ms {235006 10001}
Edit:                 60µs
reparse:              1.2ms {11 15016}
```

The full parse evaluates 235,006 rule bodies; after the edit 11 run and 15,016 results are reused.

Limits to keep in mind:

- Positions are in code points (default) or bytes, never UTF-16 units. An editor protocol that counts UTF-16 units needs
  a conversion, or a `Document` in `Bytes` mode with its own mapping.
- The whole text and memo table are in memory. The engine can discard reuse history with `ResetHistory`; generated
  Go's Node Document has no reset method.
- `Parse` returns a complete tree each time. The engine Document can report conservative changes through
  [opt-in notifications](incremental.md#change-notifications); generated Go's Node Document has no change-notification API.
- The engine Document is unavailable with `RecognizeOnly` or [streams](streaming.md). Generated Go offers the
  smaller [`-document` API](generated-go-api.md#incremental-documents--document); `-types` adds
  `Document.ParseAST`, with direct matching for grammars eligible for batch typed parsing and Node conversion
  otherwise. It does not include the history, batch-edit or snapshot APIs of [Incremental Parsing](incremental.md).
- With `-observability`, generated Go Documents can trace and profile each `Parse` or `ParseAST` invocation. Constructor options are retained defaults; per-call options override only that invocation. Events report actual calls, including a memo hit without replaying nested calls, and per-invocation profile increases in `Evals` and `MemoHits` match `Stats().Evaluated` and `.Reused` after completed parses. Same-Document operations from a callback are unsupported; see [generated Document tracing](debugging.md#tracing-and-profiling-generated-go-documents).
- A `Document` is single-threaded. Parse in the goroutine that owns it.

## Related Topics

- [What Incremental Parsing Reuses](incremental-reuse.md): writing grammars that reuse well.
- [Performance](../performance.md): current benchmark results and how to measure your grammar.
- [Debugging and Profiling](debugging.md): tracing and profiling a `Document`.
