package genrt

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func documentTestRules() (*rule, *rule) {
	descs = []string{"any", "begin input", "end input", "begin line", "end line"}
	item := &rule{id: 1, name: "item", memo: true, seen: -1}
	item.body = func(p *parser, _ int) (*Node, bool) {
		return p.matchLiteral([]rune("a"), "a", idAny, true)
	}
	main := &rule{id: 0, name: "main", memo: true, seen: -1}
	main.body = func(p *parser, _ int) (*Node, bool) {
		start := p.pos
		var children []*Node
		for {
			v, ok := p.call(item, 0)
			if !ok {
				break
			}
			children = append(children, v)
		}
		if !p.atEnd() {
			return nil, false
		}
		return &Node{kind: kindList, Start: int32(start), End: int32(p.pos), Children: children}, true
	}
	return main, item
}

func TestPrivateDocumentPrefixReuseAndFreshResult(t *testing.T) {
	main, _ := documentTestRules()
	for _, unit := range []Unit{CodePoints, Bytes} {
		d := newDocumentState(main, "aa", unit)
		o := parseOptions{unit: unit, maxDepth: defaultMaxDepth}
		first, err := d.parse(o)
		if err != nil || first == nil || len(first.Children) != 2 {
			t.Fatalf("unit %d: first parse = %v, %v", unit, first, err)
		}
		if err := d.edit(2, 2, "a"); err != nil {
			t.Fatal(err)
		}
		second, err := d.parse(o)
		if err != nil || second == nil || len(second.Children) != 3 || d.stats.Reused < 1 {
			t.Fatalf("unit %d: edited parse = %v, %v; stats = %+v", unit, second, err, d.stats)
		}
		if first.Children[0] != second.Children[0] {
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
	descs = []string{"any", "begin input", "end input", "begin line", "end line"}
	item := &rule{id: 1, name: "item", memo: true, seen: -1}
	item.body = func(p *parser, _ int) (*Node, bool) {
		start := p.pos
		v, ok := p.matchLiteral([]rune("a"), "a", idAny, true)
		if !ok {
			return nil, false
		}
		_, _, ok = p.peek() // lookahead that extends beyond the consumed text
		p.pos = start + 1
		return v, ok
	}
	main := &rule{id: 0, name: "main", memo: true, seen: -1}
	main.body = func(p *parser, _ int) (*Node, bool) { return p.call(item, 0) }
	d := newDocumentState(main, "ab", CodePoints)
	o := parseOptions{maxDepth: defaultMaxDepth}
	if _, err := d.parse(o); err == nil { // suffix remains after the rule
		t.Fatal("expected whole-input failure")
	}
	if e := d.memo.get(item.id, 0, 0, nil); e == nil || e.examined <= 1 {
		t.Fatalf("lookahead dependency was not recorded: %+v", e)
	}
	if err := d.edit(1, 2, ""); err != nil {
		t.Fatal(err)
	}
	if e := d.memo.get(item.id, 0, 0, nil); e != nil {
		t.Fatal("lookahead-dependent memo survived edit")
	}
	if _, err := d.parse(o); err == nil {
		t.Fatal("EOF made the lookahead fail; parse must not reuse the old result")
	}
}

func TestPrivateDocumentEditBoundariesAndUnit(t *testing.T) {
	main, _ := documentTestRules()
	d := newDocumentState(main, "é", Bytes)
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
	d := newDocumentState(main, "aa", CodePoints)
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
	main.body = func(*parser, int) (*Node, bool) { panic(fatal{err: ErrWorkLimit}) }
	if _, err := d.parse(o); !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("interruption = %v", err)
	}
	if len(d.memo.slots) != 0 {
		t.Fatalf("execution interruption retained %d memo slots", len(d.memo.slots))
	}
}
