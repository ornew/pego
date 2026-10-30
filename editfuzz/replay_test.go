package editfuzz

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ornew/pego"
)

var backends = []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative}
var units = []pego.PositionUnit{pego.CodePoints, pego.Bytes}

// Grammars exercising success and failure, recovered errors, left recursion, lookahead and long
// repetitions.
var testGrammars = []string{
	"@start\nrule main = item* $$\nrule item = .",
	`@start
rule main = (stmt ";")* $$
rule stmt = ("let" sp name "=" expr) #recover(skip=[^;]*)
rule name = [a-z]+ #atomic
rule expr = expr "+" term / term
rule term = [0-9]+ #atomic / "(" expr ")"
rule sp = " "*`,
	"@start\nrule main = (word / other)* $$\nrule word = &[a-z] [a-zé]+ !\"!\" #atomic\nrule other = .",
	"@start\nrule main = x $$\nrule x = x \"+\" \"a\" / \"a\"",
}

func edit(start, end int, text string) Operation {
	return Operation{Kind: Edit, Start: start, End: end, Text: []byte(text)}
}

var (
	undo       = Operation{Kind: Undo}
	redo       = Operation{Kind: Redo}
	checkpoint = Operation{Kind: Checkpoint}
)

func sequence(src string, b pego.Backend, u pego.PositionUnit, initial string, ops ...Operation) *Sequence {
	return &Sequence{GrammarSHA256: GrammarHash([]byte(src)), Start: "main", Backend: b, Unit: u, Initial: []byte(initial), Operations: ops}
}

func TestSequenceJSON(t *testing.T) {
	seq := sequence("g", pego.BytecodeIterative, pego.Bytes, "a\xc3\xa9\xff",
		edit(1, 3, "x\x80"), edit(0, 0, ""), undo, redo, checkpoint)
	seq.Seed = 41
	data, err := json.Marshal(seq)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"version":1,"grammar_sha256":"` + GrammarHash([]byte("g")) + `","grammar_normalization":"none","start":"main",` +
		`"backend":"bytecode-iterative","unit":"bytes","seed":41,"text_encoding":"base64","initial_base64":"YcOp/w==",` +
		`"operations":[{"kind":"edit","start":1,"end":3,"text_base64":"eIA="},{"kind":"edit","start":0,"end":0,"text_base64":""},` +
		`{"kind":"undo"},{"kind":"redo"},{"kind":"checkpoint"}]}`
	if string(data) != want {
		t.Fatalf("got  %s\nwant %s", data, want)
	}
	var back Sequence
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	again, _ := json.Marshal(&back)
	if string(again) != want {
		t.Fatalf("round trip changed the sequence:\n%s", again)
	}
	if string(back.Initial) != "a\xc3\xa9\xff" || string(back.Operations[0].Text) != "x\x80" {
		t.Fatalf("bytes changed: %q %q", back.Initial, back.Operations[0].Text)
	}
}

func TestSequenceJSONErrors(t *testing.T) {
	valid := `{"version":1,"grammar_sha256":"` + GrammarHash(nil) + `","grammar_normalization":"none","start":"main",` +
		`"backend":"closure","unit":"codepoints","seed":0,"text_encoding":"base64","initial_base64":"",` +
		`"operations":[OPS]}`
	var seq Sequence
	if err := json.Unmarshal([]byte(strings.Replace(valid, "OPS", "", 1)), &seq); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ old, new, want string }{
		{`"version":1`, `"version":2`, "version 2"},
		{`"grammar_normalization":"none"`, `"grammar_normalization":"format"`, "normalization"},
		{`"text_encoding":"base64"`, `"text_encoding":"utf8"`, "text encoding"},
		{`"backend":"closure"`, `"backend":"default"`, "unknown backend"},
		{`"unit":"codepoints"`, `"unit":"rune"`, "unknown unit"},
		{`"start":"main"`, `"start":""`, "start rule"},
		{`"seed":0`, `"seed":0,"extra":1`, "unknown field"},
		{`"grammar_sha256":"`, `"grammar_sha256":"x`, "grammar hash"},
		{`"initial_base64":""`, `"initial_base64":"!"`, "illegal base64"},
		{"OPS", `{"kind":"edit","start":0,"end":0}`, `missing field "text_base64"`},
		{"OPS", `{"kind":"edit","start":2,"end":1,"text_base64":""}`, "invalid edit range"},
		{"OPS", `{"kind":"edit","start":-1,"end":1,"text_base64":""}`, "invalid edit range"},
		{"OPS", `{"kind":"undo","start":0}`, "no range"},
		{"OPS", `{"kind":"checkpoint","text_base64":""}`, "no range"},
		{"OPS", `{"kind":"insert"}`, "unknown operation"},
		{"OPS", `{"kind":"undo","x":1}`, "unknown field"},
		{`"version":1`, `"VERSION":1`, `unknown field "VERSION"`},
		{`"seed":0`, `"seed":0,"seed":1`, `duplicate field "seed"`},
		{`,"operations":[OPS]`, ``, `missing field "operations"`},
		{`"initial_base64":""`, `"initial_base64":null`, `field "initial_base64" is null`},
		{`"grammar_sha256":"`, `"grammar_sha256":"` + strings.ToUpper(GrammarHash(nil)) + `x`, "lowercase hex"},
		{"OPS", `{"KIND":"undo"}`, `missing field "kind"`},
		{"OPS", `{"kind":"undo","kind":"redo"}`, `duplicate field "kind"`},
		{"OPS", `{"kind":"edit","start":0,"END":0,"end":0,"text_base64":""}`, `unknown field "END"`},
		{`}`, `}{}`, "after top-level value"},
	} {
		data := strings.Replace(strings.Replace(valid, tc.old, tc.new, 1), "OPS", "", 1)
		if err := json.Unmarshal([]byte(data), &seq); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error containing %q", tc.new, err, tc.want)
		}
	}
	if _, err := json.Marshal(sequence("g", pego.DefaultBackend, pego.Bytes, "")); err == nil {
		t.Error("a sequence with the default backend was written")
	}
	for _, bad := range []*Sequence{
		{GrammarSHA256: "nothex", Start: "main", Backend: pego.Closure},
		{GrammarSHA256: GrammarHash(nil), Backend: pego.Closure},
		sequence("g", pego.Closure, pego.Bytes, "", edit(2, 1, "")),
	} {
		if data, err := json.Marshal(bad); err == nil {
			t.Errorf("wrote %s", data)
		}
	}
	if _, err := json.Marshal(sequence("g", pego.Closure, pego.PositionUnit(7), "")); err == nil {
		t.Error("a sequence with an unknown unit was written")
	}
	if _, err := json.Marshal(sequence("g", pego.Closure, pego.Bytes, "", Operation{Kind: "bogus"})); err == nil {
		t.Error("an unknown operation was written")
	}
	// A Sequence value is written in the same format as a pointer.
	ptr := sequence("g", pego.Closure, pego.Bytes, "x", checkpoint)
	a, err := json.Marshal(ptr)
	b, err2 := json.Marshal(*ptr)
	if err != nil || err2 != nil || string(a) != string(b) {
		t.Errorf("pointer %s, %v; value %s, %v", a, err, b, err2)
	}
}

// TestReplay replays edit histories with grouped edits, undo and redo, joined characters and edits at
// both ends on every grammar, backend and unit. Every position is preceded only by ASCII characters and
// invalid bytes, so it is the same in both units.
func TestReplay(t *testing.T) {
	cases := []struct {
		initial string
		ops     []Operation
	}{
		{"let a=1+2;", []Operation{edit(0, 0, "let b=(3);"), checkpoint, edit(4, 5, "zz"), edit(0, 1, ""), checkpoint,
			undo, checkpoint, undo, undo, checkpoint, redo, redo, checkpoint}},
		// The first edit joins "\xc3" and "\xa9" into "é"; undo splits them again.
		{"a\xc3x", []Operation{edit(2, 3, "\xa9"), checkpoint, undo, checkpoint, redo, checkpoint, undo,
			edit(0, 0, "\xe2\x82"), checkpoint, edit(2, 2, "\xac"), checkpoint, undo, checkpoint}},
		{"", []Operation{edit(0, 0, "a+a+a"), checkpoint, edit(5, 5, "+a"), checkpoint, undo, undo, checkpoint, redo, checkpoint}},
		{"word! é", []Operation{checkpoint, edit(0, 0, "\x80"), edit(1, 1, ""), checkpoint, undo, undo, redo, checkpoint}},
		{"abc", []Operation{edit(3, 3, "\xc3"), checkpoint, edit(4, 4, "\xa9"), checkpoint, undo, checkpoint, undo, checkpoint}},
	}
	for gi, src := range testGrammars {
		for _, b := range backends {
			for _, u := range units {
				for _, tc := range cases {
					if err := Replay([]byte(src), sequence(src, b, u, tc.initial, tc.ops...)); err != nil {
						t.Errorf("grammar %d %v %v %q: %v", gi, b, u, tc.initial, err)
					}
				}
			}
		}
	}
}

// TestUndoRestoresText checks the texts after undo and redo, and that undoing an edit that joined
// characters edits the Document instead of starting a new one. Documents with code point positions
// replace each invalid byte with U+FFFD, so their bytes are never joined.
func TestUndoRestoresText(t *testing.T) {
	src := testGrammars[0]
	p, err := pego.CompileSource(src, "main")
	if err != nil {
		t.Fatal(err)
	}
	const r = "\uFFFD"
	for _, tc := range []struct {
		unit  pego.PositionUnit
		steps []Operation
		want  []string
	}{
		{pego.Bytes, []Operation{edit(2, 3, "\xa9"), edit(0, 0, "\xff"), undo, undo, redo, redo, undo, edit(1, 3, "b"), undo, undo},
			[]string{"aé", "\xffaé", "aé", "a\xc3x", "aé", "\xffaé", "aé", "ab", "aé", "a\xc3x"}},
		{pego.CodePoints, []Operation{edit(2, 3, "\xa9"), edit(0, 0, "\xff"), undo, undo, redo, redo, undo, edit(1, 2, "b"), undo, undo},
			[]string{"a" + r + r, r + "a" + r + r, "a" + r + r, "a" + r + "x", "a" + r + r, r + "a" + r + r, "a" + r + r, "ab" + r, "a" + r + r, "a" + r + "x"}},
	} {
		seq := sequence(src, pego.Closure, tc.unit, "a\xc3x")
		rp, err := start(p, seq)
		if err != nil {
			t.Fatal(err)
		}
		doc := rp.doc
		for i, op := range tc.steps {
			if err := rp.apply(op); err != nil {
				t.Fatalf("%v %v: %v", tc.unit, op, err)
			}
			if rp.doc.Text() != tc.want[i] {
				t.Fatalf("%v step %d %v: text %q, want %q", tc.unit, i, op, rp.doc.Text(), tc.want[i])
			}
		}
		if rp.doc != doc {
			t.Errorf("%v: undo replaced the Document", tc.unit)
		}
		// The edit to "ab" discarded the undone insertion of "\xff".
		for range 2 {
			if err := rp.apply(redo); err != nil {
				t.Fatal(err)
			}
		}
		if err := rp.apply(redo); err == nil || rp.doc.Text() != tc.want[7] {
			t.Errorf("%v: redo after a new edit: %v, text %q", tc.unit, err, rp.doc.Text())
		}
	}
}

func TestDiff(t *testing.T) {
	for _, tc := range []struct {
		cur, target string
		unit        pego.PositionUnit
		start, end  int
		text        string
	}{
		{"abc", "abc", pego.CodePoints, 3, 3, ""},
		{"aé", "a\xc3x", pego.CodePoints, 1, 2, "\xc3x"},
		{"aé", "a\xc3x", pego.Bytes, 1, 3, "\xc3x"},
		{"日本語", "日語", pego.CodePoints, 1, 2, ""},
		{"日本語", "日語", pego.Bytes, 3, 6, ""},
		{"aaa", "aa", pego.CodePoints, 2, 3, ""},
		{"", "\x80", pego.Bytes, 0, 0, "\x80"},
	} {
		start, end, text := diff(tc.cur, tc.target, tc.unit)
		if start != tc.start || end != tc.end || text != tc.text {
			t.Errorf("diff(%q, %q, %v) = %d, %d, %q; want %d, %d, %q", tc.cur, tc.target, tc.unit, start, end, text, tc.start, tc.end, tc.text)
		}
	}
}

func TestReplayInvalid(t *testing.T) {
	src := testGrammars[0]
	for _, tc := range []struct {
		seq   *Sequence
		src   string
		event int
		want  string
	}{
		{sequence(src, pego.Closure, pego.CodePoints, "a"), "rule main = .", -1, "hash"},
		{sequence("rule main = ", pego.Closure, pego.CodePoints, "a"), "rule main = ", -1, ""},
		{&Sequence{GrammarSHA256: GrammarHash([]byte(src)), Start: "other", Backend: pego.Closure}, src, -1, "rule other is not defined"},
		{&Sequence{GrammarSHA256: GrammarHash([]byte(src)), Start: "item", Backend: pego.Closure}, src, -1, "rule item is not a start rule"},
		{sequence(src, pego.Closure, pego.CodePoints, "a", checkpoint, undo), src, 1, "nothing to undo"},
		{sequence(src, pego.Closure, pego.CodePoints, "a", edit(0, 1, "b"), redo), src, 1, "nothing to redo"},
		{sequence(src, pego.Closure, pego.CodePoints, "a", edit(0, 2, "b")), src, 0, "invalid range"},
		{sequence(src, pego.Bytecode, pego.Bytes, "é", edit(1, 1, "b")), src, 0, "boundary"},
	} {
		err := Replay([]byte(tc.src), tc.seq)
		var invalid *InvalidError
		if !errors.As(err, &invalid) || invalid.Event != tc.event || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: got %v, want an invalid sequence at %d containing %q", tc.seq.Operations, err, tc.event, tc.want)
		}
	}
}

// TestMismatch injects differences into Document results and checks what the mismatch reports.
func TestMismatch(t *testing.T) {
	src := testGrammars[0]
	defer func() { corruptDocumentResult = nil }()
	for _, tc := range []struct {
		corrupt     func(map[string]any)
		path, print string
	}{
		{func(tree map[string]any) {
			tree["value"].(map[string]any)["value"].([]any)[0].(map[string]any)["value"].([]any)[1].(map[string]any)["end"] = 9
		}, "value.value[0].value[1].end", "value bytecode bytes value.value[].value[].end"},
		{func(tree map[string]any) { tree["error"] = errorTree(errors.New("lost")) }, "error", "error bytecode bytes error"},
		{func(tree map[string]any) {
			tree["value"].(map[string]any)["value"] = append(tree["value"].(map[string]any)["value"].([]any), nil)
		}, "value.value[1]", "value bytecode bytes value.value[]"},
	} {
		corruptDocumentResult = func(text string, tree map[string]any) {
			if text == "abc" {
				tc.corrupt(tree)
			}
		}
		seq := sequence(src, pego.Bytecode, pego.Bytes, "ab", checkpoint, edit(2, 2, "c"), checkpoint, checkpoint)
		err := Replay([]byte(src), seq)
		var m *Mismatch
		if !errors.As(err, &m) || m.Event != 2 || m.Text != "abc" || m.Path != tc.path || m.Fingerprint() != tc.print {
			t.Fatalf("got %v, want a mismatch at %s with fingerprint %q", err, tc.path, tc.print)
		}
		if m.Document == m.Fresh || !strings.Contains(err.Error(), "operation 2") {
			t.Fatalf("mismatch report: %v", err)
		}
	}
}

// TestPanicIsAResult checks that a panicking parse returns its panic value and that a panicking edit is
// a mismatch.
func TestPanicIsAResult(t *testing.T) {
	if tree, x := result(func() (*pego.Node, error) { panic("boom") }); tree != nil || x != "boom" {
		t.Fatalf("got %v, %q", tree, x)
	}
	if _, x := result(func() (*pego.Node, error) { return nil, nil }); x != "" {
		t.Fatalf("got %q", x)
	}
	src := testGrammars[0]
	defer func() { editHook = nil }()
	editHook = func(op Operation) {
		if string(op.Text) == "!" {
			panic("edit boom")
		}
	}
	err := Replay([]byte(src), sequence(src, pego.Closure, pego.Bytes, "ab", edit(0, 0, "x"), checkpoint, edit(1, 1, "!"), checkpoint))
	var m *Mismatch
	if !errors.As(err, &m) || m.Event != 2 || m.Path != "panic.edit" || m.Category() != "panic" || m.Text != "xab" ||
		m.Document != "edit boom" || !strings.Contains(err.Error(), "panicked at operation 2") {
		t.Fatalf("got %v", err)
	}
}

// TestInvalidUTF8Compares checks that results differing only in invalid bytes, which JSON replaces with
// U+FFFD, differ.
func TestInvalidUTF8Compares(t *testing.T) {
	p, err := pego.CompileSource(testGrammars[0], "main")
	if err != nil {
		t.Fatal(err)
	}
	parse := func(s string) map[string]any {
		tree, _ := result(func() (*pego.Node, error) { return p.Parse(s, pego.WithUnit(pego.Bytes)) })
		expand(tree)
		return tree
	}
	if path, a, b := firstDifference("", parse("a\xff"), parse("a\xfe")); path != "value_invalid_utf8[0]" || a != `"\xff"` || b != `"\xfe"` {
		t.Fatalf("difference at %q: %v, %v", path, a, b)
	}
	// A difference in the value is reported before one in invalid strings.
	if path, _, _ := firstDifference("", parse("a\xff"), parse("a\uFFFD")); !strings.HasPrefix(path, "value.") {
		t.Fatalf("difference at %q", path)
	}
	if path, _, _ := firstDifference("", parse("a\xff"), parse("a\xff")); path != "" {
		t.Fatalf("difference at %q", path)
	}
}

// TestErrorsCompareByFields checks that results with recovered and unrecovered syntax errors are
// compared field by field.
func TestErrorsCompareByFields(t *testing.T) {
	p, err := pego.CompileSource(testGrammars[1], "main")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := result(func() (*pego.Node, error) { return p.Parse("let a=1;let =2;let c=x;") })
	// The recovered errors are the same; a name beside them differs.
	b, _ := result(func() (*pego.Node, error) { return p.Parse("let b=1;let =2;let c=x;") })
	expand(a)
	expand(b)
	errs := a["error"].(map[string]any)
	if errs["kind"] != "syntax errors" || len(errs["errors"].([]any)) != 2 {
		t.Fatalf("recovered errors: %v", errs)
	}
	if path, _, _ := firstDifference("", a, b); !strings.HasPrefix(path, "value.") || !strings.HasSuffix(path, ".value") {
		t.Fatalf("difference at %q", path)
	}
	c, _ := result(func() (*pego.Node, error) { return p.Parse("let") })
	expand(c)
	if path, _, _ := firstDifference("", a, c); c["error"].(map[string]any)["kind"] == "" || path == "" {
		t.Fatalf("unrecovered error: %v", c)
	}
}

// TestExhaustedBudgetComparesNothing checks that a budget which only the fresh parse exhausts, because
// the Document reuses earlier work, is not a mismatch.
func TestExhaustedBudgetComparesNothing(t *testing.T) {
	src := testGrammars[1]
	initial := strings.Repeat("let a=1+2;", 20)
	for _, b := range backends {
		for budget := uint64(1); budget <= 4096; budget += 7 {
			seq := sequence(src, b, pego.CodePoints, initial, edit(0, 0, "let b=3;"), checkpoint)
			seq.Limits = pego.Limits{Budget: budget}
			var m *Mismatch
			if err := Replay([]byte(src), seq); errors.As(err, &m) {
				t.Fatalf("%v budget %d: %v", b, budget, err)
			}
		}
	}
}
