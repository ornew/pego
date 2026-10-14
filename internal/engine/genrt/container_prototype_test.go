package genrt

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestPrototypeDocumentEditAdmission(t *testing.T) {
	for _, unit := range []Unit{Bytes, CodePoints} {
		d := &prototypeDocument{text: "aéz", defaults: parseOptions{unit: unit}}
		end := 2
		if unit == Bytes {
			end = 3
			if err := d.edit(2, 3, "x"); err == nil || d.text != "aéz" {
				t.Fatal("interior-byte edit was admitted or mutated input")
			}
		}
		if err := d.edit(1, end, "🙂"); err != nil || d.text != "a🙂z" {
			t.Fatalf("unit %d: %q, %v", unit, d.text, err)
		}
		before := d.text
		for _, positions := range [][2]int{{-1, 0}, {3, 1}, {0, 99}} {
			if err := d.edit(positions[0], positions[1], ""); err == nil || d.text != before {
				t.Fatalf("invalid edit %v changed input", positions)
			}
		}
	}
	// Invalid bytes are separate characters and can be repaired into UTF-8.
	d := &prototypeDocument{text: "\xc3x\xa9", defaults: parseOptions{unit: Bytes}}
	if err := d.edit(1, 2, ""); err != nil || d.text != "é" {
		t.Fatalf("invalid-byte repair: %q, %v", d.text, err)
	}
	if err := d.edit(1, 1, ""); err == nil {
		t.Fatal("repaired UTF-8 interior became a legal boundary")
	}
}

func TestPrototypeDocumentOptionsOnceAndRetry(t *testing.T) {
	r := &rule{name: "main", seen: -1, body: func(p *parser, _ int) (*Node, bool) {
		p.pos = p.n
		return &Node{Text: "owned"}, true
	}}
	count := 0
	d, err := newPrototypeDocument(r, 0, "abc", func(o *parseOptions) { count++; o.unit = Bytes })
	if err != nil || count != 1 {
		t.Fatalf("construction: count %d, %v", count, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.parse(WithContext(ctx)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := d.parse(WithUnit(CodePoints)); err == nil {
		t.Fatal("unit override admitted")
	}
	if _, err := d.parse(func(o *parseOptions) { count++; o.maxDepth = 10 }); err != nil || count != 2 {
		t.Fatalf("retry: count %d, %v", count, err)
	}
	if d.defaults.control != nil || d.defaults.prepared {
		t.Fatal("document retained an invocation controller")
	}
}

func TestPrototypeMemoExaminedDependencies(t *testing.T) {
	base := prototypeMemoDependency{from: 2, examined: 8, complete: true}
	for _, tc := range []struct {
		name              string
		memo              prototypeMemoDependency
		start, end, delta int
		valid             bool
		from              int
	}{
		{"before", base, 8, 9, 3, true, 2},
		{"lookahead", base, 7, 8, 0, false, 2},
		{"shift", base, 0, 1, 2, true, 4},
		{"insertion", base, 2, 2, 1, true, 3},
		{"position", prototypeMemoDependency{from: 2, examined: 8, complete: true, positionReads: true}, 0, 1, 2, false, 2},
		{"recovery", prototypeMemoDependency{from: 2, examined: 8, complete: true, recovered: true}, 0, 1, 0, false, 2},
		{"lr", prototypeMemoDependency{from: 2, examined: 8, complete: true, provisional: true}, 9, 10, 0, false, 2},
		{"unknown", prototypeMemoDependency{}, 9, 10, 0, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, valid := tc.memo.advance(tc.start, tc.end, tc.delta)
			if valid != tc.valid || got.from != tc.from {
				t.Fatalf("got %+v, %v", got, valid)
			}
		})
	}
}

func TestPrototypeClonePreservesGraph(t *testing.T) {
	child := &Node{Text: "retained"}
	root := &Node{Children: []*Node{child, child}, Fields: Fields{{Name: "child", Value: child}}}
	child.Children = []*Node{root}
	copy := prototypeCloneNode(root, map[*Node]*Node{})
	if copy == root || copy.Children[0] == child || copy.Children[0] != copy.Children[1] || copy.Fields[0].Value != copy.Children[0] || copy.Children[0].Children[0] != copy {
		t.Fatal("graph copy lost alias/cycle identity or retained source nodes")
	}
	child.Text = "changed"
	if copy.Children[0].Text != "retained" {
		t.Fatal("copy changed with original graph")
	}
}

type prototypeReader struct {
	chunks []string
	err    error
	reads  int
}

func (r *prototypeReader) Read(p []byte) (int, error) {
	r.reads++
	if len(r.chunks) == 0 {
		return 0, r.err
	}
	chunk := r.chunks[0]
	r.chunks = r.chunks[1:]
	n := copy(p, chunk)
	if len(r.chunks) == 0 {
		return n, r.err
	}
	return n, nil
}

func TestPrototypeStreamDemandAndCompaction(t *testing.T) {
	for _, unit := range []Unit{Bytes, CodePoints} {
		reader := &prototypeReader{chunks: []string{"a\xc3", "\xa9z"}, err: io.EOF}
		s, err := newPrototypeStreamInput(reader, WithUnit(unit))
		if err != nil || reader.reads != 0 {
			t.Fatalf("eager read: %v", err)
		}
		if r, err := s.next(); r != 'a' || err != nil || reader.reads != 1 {
			t.Fatal("first character demand")
		}
		s.discard()
		if r, err := s.next(); r != 'é' || err != nil || reader.reads != 2 {
			t.Fatal("incomplete character demand")
		}
		s.discard()
		if !s.hasPrev || s.previous != 'é' {
			t.Fatal("compaction lost previous character")
		}
		if r, err := s.next(); r != 'z' || err != nil || reader.reads != 2 {
			t.Fatal("complete character caused unrelated read")
		}
		wantPosition := 3
		if unit == Bytes {
			wantPosition = 4
		}
		if s.position != wantPosition {
			t.Fatalf("position %d", s.position)
		}
		if _, err := s.next(); err != io.EOF || reader.reads != 2 {
			t.Fatal("pending EOF not preserved")
		}
	}
}

func TestPrototypeStreamErrorAndAdmission(t *testing.T) {
	want := errors.New("reader failure")
	r := &prototypeReader{chunks: []string{"x"}, err: want}
	s, err := newPrototypeStreamInput(r)
	if err != nil {
		t.Fatal(err)
	}
	if ch, err := s.next(); ch != 'x' || err != nil {
		t.Fatal("n>0,error lost bytes")
	}
	if _, err := s.next(); err != want {
		t.Fatal("reader error identity lost")
	}
	r = &prototypeReader{err: io.EOF}
	if _, err := newPrototypeStreamInput(r, WithMaxDepth(-1)); err == nil || r.reads != 0 {
		t.Fatal("invalid options read input")
	}
	s, err = newPrototypeStreamInput(strings.NewReader("\xf0\x9f"))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if ch, err := s.next(); ch != '\ufffd' || err != nil {
			t.Fatal("incomplete EOF bytes not individual characters")
		}
	}
}

func TestPrototypeEmitterNilOwnershipAndStop(t *testing.T) {
	e := prototypeEmitter[*Node]{convert: func(n *Node) (*Node, error) { return n, nil }}
	n := &Node{Text: "original"}
	if err := e.commit(nil); err != nil {
		t.Fatal(err)
	}
	if err := e.commit(n); err != nil {
		t.Fatal(err)
	}
	n.Text = "changed"
	if len(e.values) != 2 || e.values[0] != nil || e.values[1].Text != "original" {
		t.Fatal("nil or ownership lost")
	}
	want := errors.New("stop")
	calls := 0
	e.emit = func(*Node) error { calls++; return want }
	if err := e.commit(n); err != want {
		t.Fatal("callback error lost")
	}
	if err := e.commit(n); err != want || calls != 1 {
		t.Fatal("callback repeated after stop")
	}
}

func TestPrototypeEmitterPanicIsTerminal(t *testing.T) {
	want := &struct{ message string }{"callback panic"}
	calls := 0
	e := prototypeEmitter[*Node]{
		convert: func(n *Node) (*Node, error) { return n, nil },
		emit:    func(*Node) error { calls++; panic(want) },
	}
	func() {
		defer func() {
			if got := recover(); got != want {
				t.Fatalf("panic identity changed: %v", got)
			}
		}()
		_ = e.commit(nil)
	}()
	if err := e.commit(nil); err == nil || calls != 1 {
		t.Fatal("publication resumed after panic")
	}
}

func TestPrototypeStreamWorkControlBeforeRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := &prototypeReader{chunks: []string{"x"}, err: io.EOF}
	s, err := newPrototypeStreamInput(reader, WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	func() {
		defer func() {
			got, ok := recover().(fatal)
			if !ok || !errors.Is(got.err, context.Canceled) {
				t.Fatalf("wrong interruption: %+v", got)
			}
		}()
		_, _ = s.next()
	}()
	if reader.reads != 0 {
		t.Fatal("canceled cursor read input")
	}
}
