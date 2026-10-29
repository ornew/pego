package engine

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/value"
)

func dump(t *testing.T, n *value.Node, err error) string {
	t.Helper()
	data, jerr := json.Marshal(n)
	if jerr != nil {
		t.Fatal(jerr)
	}
	s := string(data)
	if err != nil {
		s += " error: " + err.Error()
	}
	return s
}

func TestDocumentMatchesFreshParse(t *testing.T) {
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		t.Run(b.String(), func(t *testing.T) { testDocumentMatchesFreshParse(t, b) })
	}
	// Record every repetition's run, so that the short repetitions of the test resume too.
	defer func(n int) { core.MinRecorded = n }(core.MinRecorded)
	core.MinRecorded = 1
	t.Run("resume", func(t *testing.T) { testDocumentMatchesFreshParse(t, Closure) })
}

func testDocumentMatchesFreshParse(t *testing.T, b Backend) {
	prog := mustCompile(t, gentest.IncrementalGrammar)
	o := ParseOptions{Backend: b}
	rng := rand.New(rand.NewSource(1))
	pieces := []string{"a", "b", "x = ", "1", "+", "2*3", "(", ")", "-", ",", "\n", "@", "q", " "}
	text := "x = 1+2\nab,cd\n@pos\n\ny = (3)*-4\n"
	doc, err := prog.NewDocumentWith("main", text, o)
	if err != nil {
		t.Fatal(err)
	}
	doc.Parse()
	for i := 0; i < 2000; i++ {
		cur := []rune(doc.Text())
		start := rng.Intn(len(cur) + 1)
		end := start + rng.Intn(min(4, len(cur)-start)+1)
		ins := ""
		for k := rng.Intn(3); k > 0; k-- {
			ins += pieces[rng.Intn(len(pieces))]
		}
		if err := doc.Edit(start, end, ins); err != nil {
			t.Fatal(err)
		}
		n, err := doc.Parse()
		got := dump(t, n, err)
		fn, ferr := prog.Parse("main", doc.Text()) // result of parsing from scratch with the closure engine
		if want := dump(t, fn, ferr); got != want {
			t.Fatalf("edit %d: [%d,%d) -> %q\ntext %q\n got  %s\n want %s", i, start, end, ins, doc.Text(), got, want)
		}
	}
}

func TestDocumentReusesResults(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, gentest.IncrementalGrammar)
	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString("x = 1+2*(3-4)\nab,cd,ef\n")
	}
	doc, err := prog.NewDocument("main", b.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Parse(); err != nil {
		t.Fatal(err)
	}
	full := doc.Stats().Evaluated
	// Replace one character in the middle.
	mid := len([]rune(doc.Text())) / 2
	for []rune(doc.Text())[mid] != '1' {
		mid++
	}
	if err := doc.Edit(mid, mid+1, "7"); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Parse(); err != nil {
		t.Fatal(err)
	}
	st := doc.Stats()
	if st.Evaluated*10 > full || st.Reused == 0 {
		t.Errorf("evaluated %d of %d rules, reused %d", st.Evaluated, full, st.Reused)
	}
}

// TestDocumentPositionalActions checks that results of position-dependent actions are
// recomputed rather than shifted past an edit.
func TestDocumentPositionalActions(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, gentest.IncrementalGrammar)
	doc, _ := prog.NewDocument("main", "@abc\n")
	doc.Parse()
	doc.Edit(0, 0, "\n\n")
	n, err := doc.Parse()
	if err != nil {
		t.Fatal(err)
	}
	pos := value.D(value.D(value.D(n).Children[0]).Children[2]).Children[0]
	if pos.Type() != "Pos" || pos.Field("At") != 3 {
		t.Errorf("got %s", pos)
	}
}

func TestDocumentEditErrors(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = "a"*`)
	doc, _ := prog.NewDocument("main", "aaa")
	for _, r := range [][2]int{{-1, 0}, {2, 1}, {0, 4}} {
		if err := doc.Edit(r[0], r[1], ""); err == nil {
			t.Errorf("%v: expected an error", r)
		}
	}
	if _, err := prog.NewDocument("nope", ""); err == nil {
		t.Error("expected an error for an undefined rule")
	}
}

// TestDocumentInputStartAnchors checks that results that depend on being at the start of the
// input (^^) or of a line (^) are not reused after text is inserted before them.
func TestDocumentInputStartAnchors(t *testing.T) {
	t.Parallel()
	for _, anchor := range []string{"^^", "^"} {
		src := "rule main = (z / a)*\nrule a = " + anchor + ` "b" / "b" "!"` + "\nrule z = \"q\""
		prog := mustCompile(t, src)
		doc, err := prog.NewDocument("main", "b")
		if err != nil {
			t.Fatal(err)
		}
		doc.Parse()
		if err := doc.Edit(0, 0, "q"); err != nil {
			t.Fatal(err)
		}
		n, err := doc.Parse()
		fn, ferr := prog.Parse("main", doc.Text())
		if got, want := dump(t, n, err), dump(t, fn, ferr); got != want {
			t.Errorf("%s: got %s, want %s", anchor, got, want)
		}
	}
}

// TestDocumentMovesReusedTrees checks reused results after random batches of edits (several
// edits between parses) on grammars with empty nodes, which can lie at an insertion point on
// either side of it, and nodes reachable twice (as a child and a field).
func TestDocumentMovesReusedTrees(t *testing.T) {
	cases := []struct {
		grammar, text string
		pieces        []string
	}{
		{strings.Replace(gentest.IncrementalGrammar, "(line / blank)*", "(line / blank / junk)*", 1) + "\nrule junk = [^\\n]* #atomic \"\\n\"",
			"x = 1+2\nab,cd\n@pos\n\ny = (3)*-4\n",
			[]string{"a", "b", "x = ", "1", "+", "2*3", "(", ")", "-", ",", "\n", "@", "q", " ", ""}},
		{`type W = { Pre node[string]; Word node[string]; Post node[string]; Gap node[string] }
rule main = (sep w:word sep)* $$
rule sep = none none
rule none = "" #atomic
rule word W = p:none x:[a-z]+ #atomic q:none g:gap => W{Pre: $p, Word: $x, Post: $q, Gap: $g}
rule gap = " "* #atomic`, "ab cd  e ", []string{"a", "b", " ", "ab ", "", "zz"}},
		{`rule main = items:(item / junk)* tail:"" #atomic $$
rule junk = [^;]* #atomic ";"
rule item = k:key ":" v:val? ";" kids:(-"," c:[0-9]* #atomic)*
rule key = v1:[a-z]* #atomic => list($v1)
rule val = e:[0-9]* #atomic => $e`, "a:1;,2,b:;:3;,;", []string{"a", "b", ":", ";", "1", ",", "x:2;", ",3", ":;", ""}},
		// Empty results with captures made in a lookahead: non-empty nodes, and empty nodes at
		// other positions, inside empty ones.
		{`type S = { K node[string] }
rule main = (item / junk)* $$
rule item = p:peek q:peek2 r:peek3 w:[a-z]+ #atomic ";"
rule peek = &k:[a-z]+ #atomic
rule peek2 = &k:[a-z]+ #atomic => S{K: $k}
rule peek3 = &([a-z]* #atomic m:"" #atomic)
rule junk = [^;]* #atomic ";"`, "ab;cd;", []string{"a", "b", ";", "x;", "", "1"}},
	}
	defer func(n int) { core.MinRecorded = n }(core.MinRecorded)
	core.MinRecorded = 1 // resume short repetitions too
	// Edits leave the last character alone: each text ends with a terminator that lets it parse.
	for ci, c := range cases {
		prog := mustCompile(t, c.grammar)
		for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
			doc, err := prog.NewDocumentWith("main", c.text, ParseOptions{Backend: b})
			if err != nil {
				t.Fatal(err)
			}
			doc.Parse()
			rng := rand.New(rand.NewSource(int64(11 + ci)))
			parsed := 0
			for i := 0; i < 400; i++ {
				var log []string
				edits := 1 + rng.Intn(3)
				if rng.Intn(20) == 0 {
					edits = 30 // many edits between parses
				}
				for k := edits; k > 0; k-- {
					n := len([]rune(doc.Text())) - 1
					start := rng.Intn(n + 1)
					end := start
					if rng.Intn(2) == 0 {
						end += rng.Intn(min(3, n-start) + 1)
					}
					ins := c.pieces[rng.Intn(len(c.pieces))]
					if err := doc.Edit(start, end, ins); err != nil {
						t.Fatal(err)
					}
					log = append(log, fmt.Sprintf("[%d,%d)->%q", start, end, ins))
				}
				n, err := doc.Parse()
				if err == nil {
					parsed++
				}
				got := dump(t, n, err)
				fn, ferr := prog.Parse("main", doc.Text())
				if want := dump(t, fn, ferr); got != want {
					t.Fatalf("grammar %d, %v, round %d: %v\ntext %q\n got  %s\n want %s", ci, b, i, log, doc.Text(), got, want)
				}
			}
			if parsed < 260 {
				t.Errorf("grammar %d, %v: only %d of 400 rounds parsed", ci, b, parsed)
			}
		}
	}
}

// TestDocumentResumeEdges checks what a resumed repetition takes from the elements it reuses
// besides their values.
func TestDocumentResumeEdges(t *testing.T) {
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		t.Run(b.String(), func(t *testing.T) { testDocumentResumeEdges(t, b) })
	}
}

func testDocumentResumeEdges(t *testing.T, backend Backend) {
	defer func(n int) { core.MinRecorded = n }(core.MinRecorded)
	core.MinRecorded = 1
	cases := []struct {
		name, grammar, text string
		pieces              []string
	}{
		// An element after the edit that examined input before it (^) is not reused.
		{"lookbehind", `rule main = item* $$
rule item = b:bol / mid
rule bol = ^ [a-z] #atomic
rule mid = [a-z] #atomic / "\n"`, strings.Repeat("ab\ncd\n", 8), []string{"\n", "x", ""}},
		// The expectations of reused elements: the error at the end includes the " " that the last
		// element expected there.
		// Runs with recovered errors or provisional results of a left recursion are not recorded.
		{"recovered", `rule main = stmt* $$
rule stmt = ([a-z]+ #atomic ";") #recover(skip=[^;]+ ";")`, strings.Repeat("ab;c1;", 8) + ";", []string{"a", "1", ";", ""}},
		{"left recursion", `rule main = a $$
rule a = b* "y"
rule b = a "x" / "z"`, strings.Repeat("zzyx", 6) + "y", []string{"z", "y", "x", ""}},
		// Bounds: a run that ends at its maximum, and a minimum met by reused elements alone.
		{"bounds", `rule main = ([a-z]+ #atomic ";" / [a-z] #repeat(1, 20) #atomic "." / "\n")* $$`, strings.Repeat("abcdefghijklmnopqrstuvwxy;\n", 3) + strings.Repeat("abcdefghijklmnopqrst.\n", 3) + "\n", []string{"a", ";", ".", "\n", ""}},
		// A cut in an element: an iteration that fails after it fails the repetition.
		{"cut", `rule main = ("<" #cut [a-z]+ #atomic ">")* [a-z<>]* #atomic $$`, strings.Repeat("<ab><cd>", 6) + "xyz", []string{"<", ">", "a", "", "<a>"}},
		// An element that calls a rule reading a variable defined before the repetition is not
		// resumed: the edit changes the variable, not the input the elements examined.
		{"variables", `var k int
rule main = h:hdr {k := len($h)} item* $$
rule hdr = "x"* #atomic
rule item = {k == 1} [a-z] #atomic / [a-z] #atomic "!"`, "x" + strings.Repeat("a!", 20) + "a", []string{"x", "", "a!"}},
		{"expectations", `rule main = item* $$
rule item = [a-z]+ #atomic " "*`, strings.Repeat("ab cd ", 8) + "!", []string{"a", " ", "", "  "}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prog := mustCompile(t, c.grammar)
			doc, err := prog.NewDocumentWith("main", c.text, ParseOptions{Backend: backend})
			if err != nil {
				t.Fatal(err)
			}
			doc.Parse()
			rng := rand.New(rand.NewSource(7))
			for i := 0; i < 400; i++ {
				n := len([]rune(doc.Text())) - 1 // leave the last character
				start := rng.Intn(n + 1)
				end := start + rng.Intn(min(2, n-start)+1)
				ins := c.pieces[rng.Intn(len(c.pieces))]
				if err := doc.Edit(start, end, ins); err != nil {
					t.Fatal(err)
				}
				got, err := doc.Parse()
				fn, ferr := prog.Parse("main", doc.Text())
				if g, w := dump(t, got, err), dump(t, fn, ferr); g != w {
					t.Fatalf("round %d: [%d,%d) -> %q\ntext %q\n got  %s\n want %s", i, start, end, ins, doc.Text(), g, w)
				}
			}
		})
	}
	// The input range reused elements examined: the last element looks four characters ahead,
	// past where the repetition ends, so the run's result depends on the "!!!!".
	t.Run("examined", func(t *testing.T) {
		prog := mustCompile(t, `rule main = run rest $$
rule run = item*
rule item = [a-z] #atomic &([a-z!] [a-z!] [a-z!] [a-z!])
rule rest = [^\n]* #atomic`)
		doc, err := prog.NewDocumentWith("main", "abcdefgh!!!!", ParseOptions{Backend: backend})
		if err != nil {
			t.Fatal(err)
		}
		doc.Parse()
		for _, e := range []core.DocEdit{{Start: 0, End: 1, Delta: 0}, {Start: 11, End: 12, Delta: 0}} {
			doc.Edit(e.Start, e.End, "z?"[e.Start/11:e.Start/11+1])
			got, err := doc.Parse()
			fn, ferr := prog.Parse("main", doc.Text())
			if g, w := dump(t, got, err), dump(t, fn, ferr); g != w {
				t.Fatalf("%q\n got  %s\n want %s", doc.Text(), g, w)
			}
		}
	})
	// Two runs of a parse that both map to the same old run: an insertion at its start puts one run
	// at the old position and the other where the edit moved it (the elements look behind, so the
	// old result of list is not moved there). Only one of them takes the old run over.
	t.Run("two runs", func(t *testing.T) {
		prog := mustCompile(t, `rule main = list "!" / "bb" list "?"
rule list = item*
rule item = ^ [ab] #atomic / [ab] #atomic`)
		doc, err := prog.NewDocumentWith("main", "aaaa?", ParseOptions{Backend: backend})
		if err != nil {
			t.Fatal(err)
		}
		doc.Parse()
		doc.Edit(0, 0, "bb")
		got, err := doc.Parse()
		fn, ferr := prog.Parse("main", doc.Text())
		if g, w := dump(t, got, err), dump(t, fn, ferr); g != w {
			t.Fatalf("%q\n got  %s\n want %s", doc.Text(), g, w)
		}
	})
	// The same before the repetition: the first element looks at the character before it, so the
	// run's result depends on the "\n".
	t.Run("examined before", func(t *testing.T) {
		prog := mustCompile(t, `rule main = pre run $$
rule pre = [xy\n]* #atomic
rule run = item*
rule item = b:bol / mid
rule bol = ^ [a-z] #atomic
rule mid = [a-z] #atomic / "\n"`)
		doc, err := prog.NewDocumentWith("main", "x\nab\ncd\nef\n", ParseOptions{Backend: backend})
		if err != nil {
			t.Fatal(err)
		}
		doc.Parse()
		for _, e := range []struct {
			start, end int
			text       string
		}{{6, 7, "z"}, {1, 2, "y"}} {
			doc.Edit(e.start, e.end, e.text)
			got, err := doc.Parse()
			fn, ferr := prog.Parse("main", doc.Text())
			if g, w := dump(t, got, err), dump(t, fn, ferr); g != w {
				t.Fatalf("%q\n got  %s\n want %s", doc.Text(), g, w)
			}
		}
	})
}

// TestDocumentResumeSequences checks edit sequences that resumed repetitions must get right on
// every backend.
func TestDocumentResumeSequences(t *testing.T) {
	defer func(n int) { core.MinRecorded = n }(core.MinRecorded)
	core.MinRecorded = 1
	cases := []struct {
		name, grammar, text string
		edits               []core.DocEdit // delta is ignored: the inserted text is ins
		ins                 []string
	}{
		// A minimum met only by reused elements: the edit is after all of them.
		{"minimum", `rule main = ("a" [a-z] #atomic)+ [0-9]* #atomic $$`, strings.Repeat("ab", 20) + "123", []core.DocEdit{{Start: 41, End: 42, Delta: 0}, {Start: 40, End: 41, Delta: 0}}, []string{"9", ""}},
		// Elements that call a rule reading a variable defined before the repetition: an edit to
		// the header changes how they parse, though not the input they examined.
		{"variables", `var k int
rule main = h:hdr {k := len($h)} item* $$
rule hdr = "x"* #atomic
rule item = {k == 1} [a-z!] #atomic / [a-z] #atomic "!"`, "x" + strings.Repeat("a!", 20), []core.DocEdit{{Start: 0, End: 1, Delta: 0}, {Start: 0, End: 0, Delta: 0}, {Start: 0, End: 0, Delta: 0}}, []string{"", "x", "x"}},
	}
	for _, c := range cases {
		for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
			t.Run(c.name+"/"+b.String(), func(t *testing.T) {
				prog := mustCompile(t, c.grammar)
				doc, err := prog.NewDocumentWith("main", c.text, ParseOptions{Backend: b})
				if err != nil {
					t.Fatal(err)
				}
				doc.Parse()
				for i, e := range c.edits {
					doc.Edit(e.Start, e.End, c.ins[i])
					got, err := doc.Parse()
					fn, ferr := prog.Parse("main", doc.Text())
					if g, w := dump(t, got, err), dump(t, fn, ferr); g != w {
						t.Fatalf("edit %d: %q\n got  %.300s\n want %.300s", i, doc.Text(), g, w)
					}
				}
			})
		}
	}
}

// TestDocumentEarlierTrees checks what happens to a tree returned by an earlier parse: nodes
// reused after an edit are moved in place (so the earlier tree shares them), and a clone keeps
// the tree as it was, sharing included.
func TestDocumentEarlierTrees(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = line* $$
rule line = x:[a-z]+ #atomic "\n"`)
	doc, err := prog.NewDocument("main", strings.Repeat("ab\n", 10))
	if err != nil {
		t.Fatal(err)
	}
	t1, _ := doc.Parse()
	snap := t1.Clone()
	before := dump(t, t1, nil)
	if dump(t, snap, nil) != before {
		t.Fatal("clone differs")
	}
	if l := value.D(value.D(snap).Children[0]).Children[3]; l.Field("x") != value.D(l).Children[0] {
		t.Error("clone does not share the captured child")
	}
	doc.Edit(0, 0, "zz\n")
	t2, _ := doc.Parse()
	old, cur := value.D(value.D(t1).Children[0]).Children[3], value.D(value.D(t2).Children[0]).Children[4]
	if old != cur || value.D(cur).Start != 12 || value.D(cur.Field("x").(*value.Node)).Start != 12 {
		t.Errorf("line 3 of the earlier tree: shared %v, at %d", old == cur, value.D(cur).Start)
	}
	if dump(t, snap, nil) != before {
		t.Error("the clone changed")
	}
}

// TestDocumentEditRegressions checks cases where resumed repetitions and lazily advanced memo
// entries went wrong, on every backend.
func TestDocumentEditRegressions(t *testing.T) {
	type step struct {
		start, end int
		text       string
		err        string // part of the error the parse after it must return, if it may differ from a fresh one
		noParse    bool   // no parse after this edit: the next edit follows it at once
	}
	cases := []struct {
		name, grammar, text string
		minRecorded         int
		o                   ParseOptions
		steps               []step
	}{
		// A run that starts inside inserted text, or after it: pasting a copy of a line (this hung).
		{"paste", `rule main = line* $$
rule line = as "\n"
rule as = a*
rule a = "a"`, strings.Repeat("a", 20) + "\n", 0, ParseOptions{}, []step{{21, 21, "\n" + strings.Repeat("a", 20) + "\n", "", false}}},
		// A run moved by an insertion, whose first element is empty and looked only behind: it must
		// not be reused as an element before the edit.
		{"run after insertion", `rule main = ("xc" / r0 / .)* $$
rule r0 = r1 #repeat(0, 2) "ab"
rule r1 = (^ "a")*`, "xxab", 1, ParseOptions{}, []step{{2, 2, "cd", "", false}}},
		// A capture in a lookahead in an element without captures of its own goes to the rule's
		// scope; such an element is not resumed.
		{"capture in lookahead", `rule main = (&c:"a" . / .)* $$`, strings.Repeat("a", 20), 0, ParseOptions{}, []step{{0, 1, "b", "", false}}},
		// Deleting up to the end of what the memo holds, with an entry kept at the start (Edit
		// panicked).
		{"delete tail", `rule main = item* rest $$
rule item = e "a"
rule e = ""
rule rest = [xy]*`, "axy", 0, ParseOptions{}, []step{{1, 2, "", "", false}}},
		{"delete error", `rule main = item* $$
rule item = b:bol / mid
rule bol = ^ [a-z] #atomic
rule mid = [a-z] #atomic / "\n"`, "ab!cd", 0, ParseOptions{}, []step{{1, 3, "", "", false}}},
		// An entry found invalid by an edit, then an aborted parse: the next lookup must not apply
		// the edits again.
		{"aborted parse", `rule main = (item / .)* $$
rule item = "(" item* ")" / "x"`, "xxxx(x)", 0, ParseOptions{MaxDepth: 30},
			[]step{{0, 0, "zz", "", true}, {7, 8, strings.Repeat("(", 50), "nesting too deep", false}, {7, 57, "x", "", false}}},
	}
	for _, c := range cases {
		for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
			t.Run(c.name+"/"+b.String(), func(t *testing.T) {
				if c.minRecorded > 0 {
					defer func(n int) { core.MinRecorded = n }(core.MinRecorded)
					core.MinRecorded = c.minRecorded
				}
				prog := mustCompile(t, c.grammar)
				o := c.o
				o.Backend = b
				doc, err := prog.NewDocumentWith("main", c.text, o)
				if err != nil {
					t.Fatal(err)
				}
				doc.Parse()
				for i, s := range c.steps {
					if err := doc.Edit(s.start, s.end, s.text); err != nil {
						t.Fatal(err)
					}
					if s.noParse {
						continue
					}
					done := make(chan string, 1)
					go func() {
						n, err := doc.Parse()
						if s.err != "" {
							if err == nil || !strings.Contains(err.Error(), s.err) {
								done <- fmt.Sprintf("error %v, want %q", err, s.err)
								return
							}
							done <- ""
							return
						}
						got := dump(t, n, err)
						fn, ferr := prog.ParseWith("main", doc.Text(), o)
						if want := dump(t, fn, ferr); got != want {
							done <- fmt.Sprintf("got  %.300s\nwant %.300s", got, want)
							return
						}
						done <- ""
					}()
					select {
					case msg := <-done:
						if msg != "" {
							t.Fatalf("step %d, text %q:\n%s", i, doc.Text(), msg)
						}
					case <-time.After(5 * time.Second):
						t.Fatalf("step %d: Parse does not return on %q", i, doc.Text())
					}
				}
			})
		}
	}
}
