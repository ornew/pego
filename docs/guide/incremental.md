# Incremental Parsing

`Parser.Parse` parses a whole text from scratch. An editor, a language server or a live preview parses a text that
changes a little at a time, and most of each new parse repeats the last. A `Document` holds the text and the memo table
of its last parse and applies edits. Individual `Edit` calls reuse unaffected results; `EditBatch` atomically
commits several replacements and starts the next parse with fresh reuse state. The result is the same as parsing
the new text from scratch.

|  |  |
|:--|:--|
| Input | A text in memory that is edited again and again |
| Engine API | `Parser.NewDocument`, `Document.Edit`, `Document.EditBatch`, `Document.Version`, `Document.Parse`, `Document.Stats`, `Document.CacheStats`, `Document.ResetHistory` |
| Grammar | Any grammar (some are much faster to reparse than others) |
| You get | A full tree for every version of the text |
| Saves | Time: unchanged parts are not parsed again |

An engine `Document` works with every backend ([Runtime](running-parsers.md)) and with both position units. It does
not work with `RecognizeOnly` (`NewDocument` returns an error). Standalone generated Go parsers support an opt-in
Document with the [`-document` option](generated-documents.md). With `-types`, they
also expose `Document.ParseAST`: eligible grammars use direct typed incremental matching, while other results use
the existing Node conversion path. Returned typed values belong to that invocation, though CST Node members may
share the mutable Document graph. The model, history APIs, performance examples and recipes in these guides describe the engine
Document. Generated Go provides `NewDocument`, `Text`, `Edit`, `Parse`, `Stats` and `Node.Clone`, without the engine's
`EditBatch`, `Version`, `Unit`, `Snapshot`, `CacheStats` or `ResetHistory` controls. Its `Stats` exposes only
`Evaluated` and `Reused`; it does not resume repetitions. If the problem is an input too big to hold, see
[Streaming](streaming.md): a `Document` is always parsed as a whole and
`#stream` has no effect on it, so the two do not combine.

To observe edits and parse outcomes for downstream indexes or highlights, enable
the opt-in [Document change notifications](document-changes.md) before editing.

The design is in [design record 007](../design/007-streaming-and-incremental-parsing.md).

The model, the API and a walkthrough with `Stats` are below. Three more guides continue: [What Incremental Parsing
Reuses](incremental-reuse.md) explains which results survive an edit and how to write grammars that keep more of them,
[Incremental Parsing Performance](incremental-performance.md) gives measurements and limits, and [Document Change
Notifications](document-changes.md) shows how a tool observes edits and parse outcomes. [Fuzzing
Edits](sampling-and-fuzzing.md#fuzzing-edits) checks a grammar against fresh parses with random edits.

- [The model](#the-model)
- [The API](#the-api)
- [Keeping a result while editing](#keeping-a-result-while-editing)
- [Managing reuse history](#managing-reuse-history)
- [A walkthrough with `Stats`](#a-walkthrough-with-stats)

## The Model

A `Document` is a text plus the memo table of its last parse. The memo table has an entry for every rule call (with one exception, below): the rule,
the position, and the result, together with **the range of input the call looked at** (the matched text, plus every
character that was examined to decide, such as the character after a `+` that ended it, and lookahead). When the text is
edited, an entry stays valid if the edit lies outside the range it examined, and is dropped otherwise. The next `Parse`
runs the grammar from the start rule as usual, but each rule call that finds a valid entry returns its result without
running. A long repetition is not even run element by element: it **resumes** its run from the last parse (see
[What is reused, and why](incremental-reuse.md#what-is-reused-and-why)). The result is always the same as parsing the new text from scratch. For an application-level check, compare the tree, positions and syntax errors after representative edits with a fresh parse.

Intermediate calls in an indirect left recursion can depend on an unfinished
seed whose eventual input range is not part of the call's own range. These
results, and calls that depend on them, are recomputed after any edit. A completed head retains
ordinary range-based reuse when it only used its own growing seed and no
completed seed-dependent calls. This includes independent nested heads. Calls
that read an outer growing seed or completed dependent entries can have less
reuse.

## The API

| | |
|:--|:--|
| `p.NewDocument(text, opts...)` | A document holding `text`. `WithUnit` (also the unit of `Edit`) and `WithBackend` fix its mode; `WithContext` and `WithWorkLimits` set defaults for initialization and later parses. Fails for `RecognizeOnly`, an unavailable backend, an unknown start rule, or interrupted initialization. Nothing is parsed yet. |
| `doc.Edit(start, end, text)` | Replace `[start, end)` with `text`, in the document's unit. `start == end` inserts, an empty `text` deletes. An error if the range is outside the text, and with `Bytes`, if `start` or `end` is inside a character. Does not parse. |
| `doc.Parse(opts...)` | Parse the current text using construction defaults plus invocation overrides. Counters reset per call. Results and errors are those of `Parser.Parse`, including recovered `SyntaxErrors`. |
| `doc.Snapshot(opts...)` | Parse and capture the current revision as an immutable read-only result. Ordinary syntax and recovered errors are stored in the Snapshot; interruption or an option that changes the fixed unit/backend/mode returns no Snapshot. |
| `snapshot.Root()`, `snapshot.Text()`, `snapshot.Error()` | Read the captured root, exact text and diagnostic for that revision. The Snapshot does not move when the Document changes. |
| `snapshot.Materialize()` | Make an independent mutable `*Node` graph for existing Node consumers. |
| `doc.Stats()` | Last-parse `ParseStats`: `Evaluated` counts rule bodies run; `MemoHits` and `ResumedElements` count the two kinds of reuse, and `Reused` remains their sum. |
| `doc.CacheStats()` | Read-only, point-in-time reuse-cache counts and an approximate size of its retained backing storage. |
| `doc.ResetHistory()` | Drop memo, repetition and reusable parser history while preserving current text, version, construction options and last-parse statistics. The next parse starts fresh. |
| `doc.Text()` | The current text. |
| `doc.Version()` / `doc.Unit()` | The opaque current text revision and the fixed position unit. A new document starts at sequence 1; parsing does not advance it. |
| `doc.EditBatch(base, edits...)` | Apply a batch of nonoverlapping replacements in coordinates from `base`. A stale/foreign version or invalid edit rejects the entire batch. A successful nonempty batch advances one revision and does not parse. |

Recovered parser-produced syntax diagnostics returned by `Document.Parse` are owned by that call: each `SyntaxError`
and its `Expected` and `Messages` slices are copied before publication. Ordinary parse failures are newly constructed.
Caller-supplied errors such as `Context.Err` retain their identity. Mutating a returned parser diagnostic cannot change
memoized errors or a later parse. This does not detach the returned `Node` graph, which may share nodes with the mutable
document. Parsing and diagnostic publication use the same invocation controls; if publication is interrupted, `Parse`
returns no result and clears reusable state needed for a clean retry.

In byte mode, only the interior of a valid UTF-8 character is excluded as an edit boundary. Each invalid byte is
a separate replacement character, so it can be replaced or have text inserted before or after it. An edit can join
an incomplete prefix and continuation bytes into a valid character; the next parse invalidates results that depended
on the earlier decoding, including a prefix completed by an insertion at EOF. In code-point mode, invalid bytes
have already been replaced with U+FFFD; editing that normalized text does not reassemble the original bytes.

A `Document` is not safe for concurrent use; a `Parser` is, so use one `Document` per goroutine and share the `Parser`.
Several `Edit` calls can precede one `Parse`. Each successful `Edit` advances `Version` once, including a no-op; parsing does not. Both `Edit` and `EditBatch` are outside parse-work limits.

Use `EditBatch` when one editor transaction contains several replacements. All ranges are measured in the unchanged base text, not adjusted after earlier edits in the batch. Starts must be strictly increasing and ranges nonoverlapping; insertions at the same position are rejected. A failed batch changes nothing. A successful nonempty batch advances the version once, clears reusable parse state, and leaves parsing to the next `Parse` call. It preserves the existing `Node` behavior: previously returned Nodes are not moved until a later parse. Cache reset means batches do not promise the reuse cost of a sequence of `Edit` calls.

```go
base := doc.Version()
err := doc.EditBatch(base,
    pego.TextEdit{Start: 0, End: 2, Text: "xy"},
    pego.TextEdit{Start: 3, End: 3, Text: "z"},
)
if err != nil {
    return err
}
node, err := doc.Parse()
```

The example assumes `doc` and the `pego` import are already in scope. The insertion at 3 is interpreted against the original text, even though the earlier replacement is also in the batch.

Piece-backed storage is internal to `Document`; batch parsing and streaming keep
their contiguous input paths. `Document.Text()` and `Node.Text` keep their
existing meanings. CodePoints initialization decodes and normalizes input, so
that inspected work is charged. For Bytes, a large immutable input may be kept
as an original string-backed piece without scanning or copying it; zero
construction input-work is valid when no elements were inspected. Parsing still
charges scans, literal comparisons and text materialization across pieces.

A pre-execution option error or cancellation leaves existing reusable caches and
the last `Stats` intact. Once parse execution starts, an interruption clears
the whole memo and repetition caches because completed entries can depend on
unfinished left-recursion seeds. Fully completed input tables may remain; partial
line or offset tables are never published and are discarded. The text and
options stay available for retry or editing. A fresh context and budget can
retry the parse; ordinary syntax errors and recovered errors are completed
outcomes and keep their reusable caches. Trace panics still propagate after
cleanup.

`WithContext` cooperatively cancels engine work, and `WithWorkLimits` bounds
per-invocation input work and execution steps. These are not elapsed-time or
buffered-byte limits. A blocked reader or callback cannot be preempted by the
parser; cancellation is observed after caller code returns. See [Streaming](streaming.md#cancelling-a-stream)
for the reader ownership boundary and [Runtime](running-parsers.md) for option names and
error handling.

## Keeping a Result While Editing

A `Document` reuses and moves legacy `*Node` values on later parses. If a UI
must keep the exact tree, text, diagnostics, and revision visible while the
Document advances, capture them together:

```go
snapshot, err := doc.Snapshot(pego.WithContext(ctx), pego.WithWorkLimits(limits))
if snapshot == nil {
    return err // cancellation or a work limit publishes no partial snapshot
}

text := snapshot.Text()
version := snapshot.Version()
root := snapshot.Root()
parseErr := snapshot.Error() // ordinary syntax/recovery errors accompany a snapshot
if root.Valid() {
    fmt.Println(root.Type(), root.Start(), root.End())
}
```

A normal syntax failure still returns a Snapshot with its text and version, a
zero root, and the copied syntax error. A recovered parse keeps its root and
recovered errors. Cancellation or work-limit exhaustion during parsing or
capture returns a nil Snapshot. The same invocation budget covers parsing,
copying text, freezing the graph, and copying diagnostics. Snapshot getters and
`Materialize` run after capture and have no inherited controller or work limit.

`SnapshotNode` is a read-only handle. Its zero value represents a nil Node;
`ChildCount` includes nil child slots. `Field(name)` returns both a value and a
presence flag, distinguishing an absent field from a present nil value. A typed
nil `*Node` field becomes a zero handle, while an untyped nil remains nil.
`Fields()` returns an owned slice in original order. Snapshots and handles can
be read concurrently while the single-owner Document is edited or parsed.

The snapshot graph preserves shared nodes and cycles. It freezes only field
values of dynamic type nil, `int`, `string`, `bool`, or `*pego.Node`; an
unsupported custom value returns `*pego.SnapshotCaptureError` and no Snapshot.
`Materialize` makes an independently mutable graph, preserving identities,
cycles, nil slots, and nil versus allocated-empty collections. Use it when an
existing API requires `*Node`; each call creates a separate graph.

## Managing Reuse History

An editor can choose when to release a document's incremental reuse history. For example, after a large replacement or when a file has been idle, call `doc.ResetHistory()` and let the next `doc.Parse()` build a fresh memo. This is an explicit policy decision; the parser applies no automatic byte threshold. Reset keeps the text, its version, the selected unit and backend, construction defaults and the last parse's `Stats`. Earlier returned trees stop moving through future reuse after reset, though movement from parses before reset is not undone. See [design record 035](../design/035-incremental-cache-controls.md) for the cache-control contract.

```go
before := doc.CacheStats()
doc.ResetHistory()
after := doc.CacheStats() // all four fields are zero immediately after reset
_, err := doc.Parse()      // parse the current text with fresh reuse history
_ = before
_ = after
_ = err
```

`CacheStats` describes retained history at the time of inspection:

| Field | Meaning |
|:--|:--|
| `LiveMemoEntries` | Memo entries currently linked for reuse. |
| `RetiredMemoEntries` | Retired memo entries retained for reuse. |
| `RepetitionElements` | Elements retained in resumable repetition runs. |
| `EstimatedBytes` | Approximate cache-owned backing storage. |

`EstimatedBytes` excludes document text, caller-held result trees, shared strings, shared expectation/error/environment payloads, map buckets and allocator overhead. It is neither process heap use nor a memory cap. Inspection traverses the cache and should be done by the same owner that edits and parses the document. `Stats` describes the previous parse instead: `Reused` remains the sum of `MemoHits` and `ResumedElements` for that parse.

## A Walkthrough With `Stats`

```go
package main

import (
	"fmt"
	"log"

	"github.com/ornew/pego"
)

const grammar = `rule main =  line* $$
rule line =  [a-z]+ "\n" `

func main() {
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		log.Fatal(err)
	}
	doc, err := p.NewDocument("ab\ncd\nef\n")
	if err != nil {
		log.Fatal(err)
	}

	step := func(what string) {
		node, err := doc.Parse()
		st := doc.Stats()
		if err != nil {
			fmt.Printf("%-28s {Evaluated:%d Reused:%d}  error: %v\n", what, st.Evaluated, st.Reused, err)
			return
		}
		fmt.Printf("%-28s {Evaluated:%d Reused:%d}  %v\n", what, st.Evaluated, st.Reused, node)
	}

	step("first parse")
	step("parse again")
	doc.Edit(3, 4, "x") // "ab\nxd\nef\n"
	step("edit inside line 2")
	doc.Edit(0, 0, "zz\n") // "zz\nab\nxd\nef\n"
	step("insert a line at the start")
	doc.Edit(0, 3, "") // delete it again
	step("delete it again")
	doc.Edit(1, 2, "!") // "a!\nxd\nef\n"
	step("break line 1")
}
```

```text
first parse                  {Evaluated:5 Reused:0}  (Seq [(Seq ["a" "b"] "\n")@line (Seq ["c" "d"] "\n")@line (Seq ["e" "f"] "\n")@line])@main
parse again                  {Evaluated:0 Reused:1}  (Seq [(Seq ["a" "b"] "\n")@line (Seq ["c" "d"] "\n")@line (Seq ["e" "f"] "\n")@line])@main
edit inside line 2           {Evaluated:2 Reused:3}  (Seq [(Seq ["a" "b"] "\n")@line (Seq ["x" "d"] "\n")@line (Seq ["e" "f"] "\n")@line])@main
insert a line at the start   {Evaluated:2 Reused:4}  (Seq [(Seq ["z" "z"] "\n")@line (Seq ["a" "b"] "\n")@line (Seq ["x" "d"] "\n")@line (Seq ["e" "f"] "\n")@line])@main
delete it again              {Evaluated:0 Reused:1}  (Seq [(Seq ["a" "b"] "\n")@line (Seq ["x" "d"] "\n")@line (Seq ["e" "f"] "\n")@line])@main
break line 1                 {Evaluated:2 Reused:0}  error: 1:2: syntax error: expected "\n", [a-z]
```

- **First parse: 5 evaluations.** `main`, three `line`s and the fourth attempt at `line`, which fails at the end of the
  text. Failures are memoized too.
- **Parsing again: 0 evaluations, 1 reuse.** The entry for `main` at position 0 is valid, so the start rule is not even
  run.
- **Editing inside line 2** (`[3, 4)` replaced): 2 evaluations (`main` and the second `line`) and 3 reuses. The first
  line examined only `[0, 3)`, which ends where the edit starts, so it is kept as is. The third line and the failing
  attempt at the end lie after the edit, so they are kept with their positions shifted by the length change (zero here).
  `main` examined the whole text and the second line contains the edit, so both run again.
- **Inserting a line at the start:** `main` and the new line run; the three old lines and the failing attempt at the
  end are reused at their new positions.
- **Deleting it again: 0 evaluations.** The entry for the old `main`, shifted away by the insertion, was shifted back by
  the deletion and is exactly right again. Entries after an edit survive even if the parse after it did not use them,
  so undoing an edit can cost nothing.
- **Breaking line 1:** the result is the syntax error, as from a fresh parse. Nothing after the error is examined, so the
  rest of the memo table is simply left for later.

Reused results are not copied. Unchanged subtrees before an edit are shared with the new tree as they are, and
subtrees after it are shared too, **moved in place** to their new positions. A tree returned by an earlier `Parse`
therefore changes: the nodes the new parse reused carry their new positions. To keep a tree as it was (for example,
to keep showing the last good state), take a `Clone` before the next `Parse`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ornew/pego"
)

func js(n *pego.Node) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func main() {
	p, _ := pego.CompileSource(`rule main =  line* $$
rule line =  [a-z]+ "\n" `, "main")
	d, _ := p.NewDocument(strings.Repeat("ab\n", 100))
	t1, _ := d.Parse()
	snapshot := t1.Clone()
	before := js(t1)
	d.Edit(0, 0, "zzzz\n")
	t2, _ := d.Parse()
	l1, l2 := t1.Children[0].Children, t2.Children[0].Children
	fmt.Println("line 50 shared:", l1[50] == l2[51], "now starts at", l2[51].Start)
	fmt.Println("earlier tree unchanged:", js(t1) == before)
	fmt.Println("snapshot unchanged:", js(snapshot) == before)
}
```

```text
line 50 shared: true now starts at 155
earlier tree unchanged: false
snapshot unchanged: true
```

Pointer comparison tells which nodes the new tree reused from the old one (`l1[50] == l2[51]`); compare `Start`, `End`
and content to see what else is the same. After the next `Parse`, an earlier tree is a mix: the nodes the new tree
reused are at their new positions, the others at their old ones, so a child can even lie outside its parent's range.
Use it only for such comparisons, or keep a `Clone`, which copies the whole tree (keeping subtrees shared within it
shared) and costs about as much memory as the tree. `Parse` writes the nodes it moves, so do not read an earlier
tree while `Parse` runs.

## Related Topics

- [What Incremental Parsing Reuses](incremental-reuse.md): which results survive an edit.
- [Document Change Notifications](document-changes.md): observing edits and parse outcomes.
- [Streaming](streaming.md): input too big to hold.
- [Running Parsers](running-parsers.md): backends, position units and the other parse options.
