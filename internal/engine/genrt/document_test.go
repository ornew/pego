package genrt

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func documentTestRules() (*Rule, *Rule) {
	item := &Rule{Id: 1, Name: "item", Memo: true, DocMemo: true, Seen: -1}
	item.Body = func(p *Parser, _ int) (*Node, bool) {
		return p.MatchLiteral([]rune("a"), "a", IdAny, true)
	}
	main := &Rule{Id: 0, Name: "main", Memo: true, DocMemo: true, Seen: -1}
	main.Body = func(p *Parser, _ int) (*Node, bool) {
		start := p.Pos
		var children []*Node
		for {
			v, ok := p.Call(item, 0)
			if !ok {
				break
			}
			children = append(children, v)
		}
		if !p.AtEnd() {
			return nil, false
		}
		return &Node{RawKind: RawKindList, RawStart: int32(start), RawEnd: int32(p.Pos), RawChildren: children}, true
	}
	return main, item
}

func TestPrivateDocumentPrefixReuseAndFreshResult(t *testing.T) {
	main, _ := documentTestRules()
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		d := testDocumentState(main, "aa", unit)
		o := parseOptions{unit: unit, maxDepth: defaultMaxDepth}
		first, err := d.parse(o)
		if err != nil || first == nil || len(first.RawChildren) != 2 {
			t.Fatalf("unit %d: first parse = %v, %v", unit, first, err)
		}
		if err := d.edit(2, 2, "a"); err != nil {
			t.Fatal(err)
		}
		second, err := d.parse(o)
		if err != nil || second == nil || len(second.RawChildren) != 3 || d.stats.Reused < 1 {
			t.Fatalf("unit %d: edited parse = %v, %v; stats = %+v", unit, second, err, d.stats)
		}
		if first.RawChildren[0] != second.RawChildren[0] {
			t.Fatalf("unit %d: safe prefix memo was not reused", unit)
		}
		if err := d.edit(0, 1, "b"); err != nil {
			t.Fatal(err)
		}
		if _, err := d.parse(o); err == nil {
			t.Fatalf("unit %d: changed prefix unexpectedly parsed", unit)
		}
	}
}

func TestPrivateDocumentExaminedSuffixAndEOF(t *testing.T) {
	item := &Rule{Id: 1, Name: "item", Memo: true, DocMemo: true, Seen: -1}
	item.Body = func(p *Parser, _ int) (*Node, bool) {
		start := p.Pos
		v, ok := p.MatchLiteral([]rune("a"), "a", IdAny, true)
		if !ok {
			return nil, false
		}
		_, _, ok = p.Peek() // lookahead that extends beyond the consumed text
		p.Pos = start + 1
		return v, ok
	}
	main := &Rule{Id: 0, Name: "main", Memo: true, DocMemo: true, Seen: -1}
	main.Body = func(p *Parser, _ int) (*Node, bool) { return p.Call(item, 0) }
	d := testDocumentState(main, "ab", CodePoints)
	o := parseOptions{maxDepth: defaultMaxDepth}
	if _, err := d.parse(o); err == nil { // suffix remains after the rule
		t.Fatal("expected whole-input failure")
	}
	if e := d.memo.get(item.Id, 0, 0, nil); e == nil || e.examined <= 1 {
		t.Fatalf("lookahead dependency was not recorded: %+v", e)
	}
	if err := d.edit(1, 2, ""); err != nil {
		t.Fatal(err)
	}
	if e := d.memo.get(item.Id, 0, 0, nil); e != nil {
		t.Fatal("lookahead-dependent memo survived edit")
	}
	if _, err := d.parse(o); err == nil {
		t.Fatal("EOF made the lookahead fail; parse must not reuse the old result")
	}
}

func TestPrivateDocumentEditBoundariesAndUnit(t *testing.T) {
	main, _ := documentTestRules()
	d := testDocumentState(main, "é", Bytes)
	if err := d.edit(1, 1, "x"); err == nil || d.text != "é" {
		t.Fatalf("interior UTF-8 edit = %v, text = %q", err, d.text)
	}
	if err := d.edit(0, 0, "\xff"); err != nil || !strings.HasPrefix(d.text, "\xff") {
		t.Fatalf("invalid byte insertion = %v, text = %q", err, d.text)
	}
	if _, err := d.parse(parseOptions{unit: CodePoints, maxDepth: defaultMaxDepth}); err == nil || !strings.Contains(err.Error(), "unit") {
		t.Fatalf("unit change = %v", err)
	}
	if err := d.edit(-1, 0, "x"); err == nil {
		t.Fatal("invalid range was accepted")
	}
}

func TestPrivateDocumentInterruptionDiscardsMemo(t *testing.T) {
	main, _ := documentTestRules()
	d := testDocumentState(main, "aa", CodePoints)
	o := parseOptions{maxDepth: defaultMaxDepth}
	if _, err := d.parse(o); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.parse(parseOptions{maxDepth: defaultMaxDepth, context: ctx}); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-execution interruption = %v", err)
	}
	if len(d.memo.slots) == 0 {
		t.Fatal("pre-execution interruption discarded complete memo")
	}
	if err := d.edit(2, 2, "a"); err != nil {
		t.Fatal(err)
	}
	main.Body = func(*Parser, int) (*Node, bool) { panic(fatal{err: ErrWorkLimit}) }
	if _, err := d.parse(o); !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("interruption = %v", err)
	}
	if len(d.memo.slots) != 0 {
		t.Fatalf("execution interruption retained %d memo slots", len(d.memo.slots))
	}
}

func TestDocumentReturnedRecoveryErrorOwnership(t *testing.T) {
	r := &Rule{Id: 0, Name: "main", Memo: true, DocMemo: true, Seen: -1}
	r.Body = func(p *Parser, _ int) (*Node, bool) {
		p.Pos = 1
		p.Recovered = append(p.Recovered, &SyntaxError{Line: 1, Col: 1, Expected: []string{"original"}, Messages: []string{"original"}})
		return &Node{RawKind: kindError, RawEnd: 1}, true
	}
	d := testDocumentState(r, "x", CodePoints)
	o := parseOptions{maxDepth: defaultMaxDepth}
	_, first := d.parse(o)
	e, ok := first.(SyntaxErrors)
	if !ok || len(e) != 1 {
		t.Fatal(first)
	}
	e[0].Messages[0] = "mutated"
	e[0].Expected[0] = "mutated"
	_, second := d.parse(o)
	next, ok := second.(SyntaxErrors)
	if !ok || len(next) != 1 {
		t.Fatal(second)
	}
	if next[0].Messages[0] != "original" || next[0].Expected[0] != "original" {
		t.Fatalf("caller mutation changed cached diagnostics: %+v", next[0])
	}
}

func TestDocumentDiagnosticPublicationBudget(t *testing.T) {
	calls := 0
	r := &Rule{Id: 0, Name: "main", Memo: true, DocMemo: true, Seen: -1}
	r.Body = func(p *Parser, _ int) (*Node, bool) {
		calls++
		p.Pos = 1
		expected := make([]string, 2048)
		for i := range expected {
			expected[i] = "original"
		}
		p.Recovered = append(p.Recovered, &SyntaxError{Line: 1, Col: 1, Expected: expected, Messages: []string{"original"}})
		return &Node{RawKind: kindError, RawEnd: 1}, true
	}
	d := testDocumentState(r, "x", CodePoints)
	normal := parseOptions{maxDepth: defaultMaxDepth}
	if _, err := d.parse(normal); err == nil {
		t.Fatal("missing recovered diagnostic")
	}
	if calls != 1 {
		t.Fatalf("initial evaluations: %d", calls)
	}
	limited := normal
	limited.limits = WorkLimits{MaxSteps: 64}
	if out, err := d.parse(limited); out != nil || !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("diagnostic budget: %v %v", out, err)
	}
	if calls != 1 {
		t.Fatal("warm parse should reuse memo before publication")
	}
	if len(d.memo.slots) != 0 {
		t.Fatal("interrupted publication retained memo")
	}
	if _, err := d.parse(normal); err == nil {
		t.Fatal("retry lost recovered diagnostic")
	}
	if calls != 2 {
		t.Fatal("retry failed to rebuild cleared memo")
	}
}

type diagnosticCancelContext struct {
	context.Context
	done  chan struct{}
	polls int
}

func (c *diagnosticCancelContext) Done() <-chan struct{} { return c.done }
func (c *diagnosticCancelContext) Err() error {
	c.polls++
	if c.polls == 4 {
		close(c.done)
	}
	if c.polls >= 4 {
		return context.Canceled
	}
	return nil
}

func TestDocumentDiagnosticPublicationCancellation(t *testing.T) {
	r := &Rule{Id: 0, Name: "main", Memo: true, DocMemo: true, Seen: -1}
	r.Body = func(p *Parser, _ int) (*Node, bool) {
		p.Pos = 1
		p.Recovered = append(p.Recovered, &SyntaxError{Expected: make([]string, 2048)})
		return &Node{RawKind: kindError, RawEnd: 1}, true
	}
	d := testDocumentState(r, "x", CodePoints)
	normal := parseOptions{maxDepth: defaultMaxDepth}
	if _, err := d.parse(normal); err == nil {
		t.Fatal("missing recovered diagnostic")
	}
	ctx := &diagnosticCancelContext{Context: context.Background(), done: make(chan struct{})}
	o := normal
	o.context = ctx
	converted := false
	out, err := DocumentParseConverted(d, o, func(n *Node, _ *WorkControl, _ error) *Node {
		converted = true
		return n
	})
	if out != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("diagnostic cancellation: %v %v", out, err)
	}
	if converted {
		t.Fatal("diagnostic copying did not poll cancellation before conversion")
	}
	if len(d.memo.slots) != 0 {
		t.Fatal("cancelled publication retained memo")
	}
}

// defaultMaxDepth is the rule-call limit of the test grammars.
const defaultMaxDepth = 100_000

// testDocumentState gives root a grammar with the fixed expectations and
// returns a document state for it.
func testDocumentState(root *Rule, text string, unit PositionUnit) *documentState {
	g := NewGrammar(defaultMaxDepth)
	g.Descs = []string{"any", "begin input", "end input", "begin line", "end line"}
	root.g = g
	return newDocumentState(root, text, unit)
}
