package engine

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// keyValue builds the tree of `rule main = k:key ":" v:num` over "a:1", where key and num are
// rules of one-element repetitions, with source information when src is set.
func keyValue(src bool) *GNode {
	key := newGNode(0, 1, newList(newGNode(0, 1, "a")))
	colon := newGNode(1, 2, ":")
	num := newGNode(2, 3, newList(newGNode(2, 3, "1")))
	main := newGNode(0, 3, newTuple(key, colon, num))
	if src {
		key.src = &nodeSource{rule: "key"}
		num.src = &nodeSource{rule: "num"}
		main.src = &nodeSource{rule: "main", caps: []nodeCapture{
			{name: "k", path: []int{0}},
			{name: "v", path: []int{2}},
		}}
	}
	return main
}

func TestGNodeAccess(t *testing.T) {
	n := keyValue(true)
	if n.Start() != 0 || n.End() != 3 || n.Len() != 3 {
		t.Fatalf("range or length: %d %d %d", n.Start(), n.End(), n.Len())
	}
	if got := n.Elem(1).(*GNode).Value(); got != ":" {
		t.Fatalf("Elem(1) = %v", got)
	}
	if n.Rule() != "main" {
		t.Fatalf("Rule = %q", n.Rule())
	}
	if got := strings.Join(n.CaptureNames(), ","); got != "k,v" {
		t.Fatalf("CaptureNames = %s", got)
	}
	v, ok := n.Capture("v")
	if !ok || v.(*GNode).Rule() != "num" {
		t.Fatalf("Capture(v) = %v, %v", v, ok)
	}
	if _, ok := n.Capture("x"); ok {
		t.Fatal("Capture(x) found")
	}
	if _, ok := keyValue(false).Capture("k"); ok {
		t.Fatal("capture found without source information")
	}
	if keyValue(false).Rule() != "" {
		t.Fatal("rule without source information")
	}
}

func TestGNodeCaptureLocations(t *testing.T) {
	// (y:b c / y:d) e, where the first alternative matched: y is inside the nested tuple.
	b, c, e := newGNode(0, 1, "b"), newGNode(1, 2, "c"), newGNode(2, 3, "e")
	n := newGNode(0, 3, newTuple(newTuple(b, c), e))
	n.src = &nodeSource{caps: []nodeCapture{{name: "y", path: []int{0, 0}}}}
	if v, _ := n.Capture("y"); v != b {
		t.Fatalf("nested capture = %v", v)
	}
	// An optional that did not match leaves its captures nil.
	opt := newGNode(0, 1, newTuple(nil, e))
	opt.src = &nodeSource{caps: []nodeCapture{{name: "y", path: []int{0, 0}}}}
	if v, ok := opt.Capture("y"); !ok || v != nil {
		t.Fatalf("capture in an unmatched optional = %v, %v", v, ok)
	}
	// A capture in a lookahead is kept beside the tuple.
	side := newGNode(2, 2, newTuple())
	m := newGNode(0, 2, newTuple(b))
	m.src = &nodeSource{caps: []nodeCapture{{name: "e", side: true, value: side}}}
	if v, _ := m.Capture("e"); v != side {
		t.Fatalf("lookahead capture = %v", v)
	}
}

func TestValueJSON(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want string
	}{
		{"string node", newGNode(0, 1, "a"), `{"start":0,"end":1,"value":"a"}`},
		{"tuple without source", keyValue(false),
			`{"start":0,"end":3,"value":[` +
				`{"start":0,"end":1,"value":[{"start":0,"end":1,"value":"a"}]},` +
				`{"start":1,"end":2,"value":":"},` +
				`{"start":2,"end":3,"value":[{"start":2,"end":3,"value":"1"}]}]}`},
		{"tuple with source", keyValue(true),
			`{"start":0,"end":3,"value":[` +
				`{"start":0,"end":1,"value":[{"start":0,"end":1,"value":"a"}],"rule":"key"},` +
				`{"start":1,"end":2,"value":":"},` +
				`{"start":2,"end":3,"value":[{"start":2,"end":3,"value":"1"}],"rule":"num"}],` +
				`"rule":"main","captures":{"k":{"path":[0]},"v":{"path":[2]}}}`},
		{"unit and nil", newGNode(0, 0, newTuple(nil, newTuple())), `{"start":0,"end":0,"value":[null,[]]}`},
		{"struct", &Struct{typ: "Op", fields: []structField{{"R", 2}, {"L", 1}, {"Ok", true}}},
			`{"type":"Op","fields":{"L":1,"Ok":true,"R":2}}`},
		{"error", &ErrorValue{start: 3, end: 5, msg: "skipped"}, `{"start":3,"end":5,"error":"skipped"}`},
		{"missing", &ErrorValue{start: 3, end: 3, msg: "missing", expected: `";"`},
			`{"start":3,"end":3,"error":"missing","expected":"\";\""}`},
		{"lookahead capture", func() any {
			n := newGNode(0, 1, newTuple(newGNode(0, 1, "a")))
			n.src = &nodeSource{caps: []nodeCapture{{name: "e", side: true, value: newGNode(1, 1, newTuple())}}}
			return n
		}(), `{"start":0,"end":1,"value":[{"start":0,"end":1,"value":"a"}],` +
			`"captures":{"e":{"value":{"start":1,"end":1,"value":[]}}}}`},
		{"list of ints", newList(1, 2), `[1,2]`},
	}
	for _, c := range cases {
		var b bytes.Buffer
		if err := writeValueJSON(&b, c.v); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if b.String() != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, b.String(), c.want)
		}
		if !json.Valid(b.Bytes()) {
			t.Errorf("%s: invalid JSON %s", c.name, b.String())
		}
	}
}

func TestValueString(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want string
	}{
		{"without source", keyValue(false), `(["a"] ":" ["1"])`},
		{"with source", keyValue(true), `(k:["a"]@key ":" v:["1"]@num)@main`},
		{"unit, nil and scalars", newGNode(0, 0, newTuple(nil, newTuple(), newList(1, true))), `(nil () [1 true])`},
		{"struct", &Struct{typ: "Op", fields: []structField{{"R", "x"}, {"L", 1}}}, `Op{L: 1, R: "x"}`},
		{"error", &ErrorValue{msg: "skipped"}, `error("skipped")`},
		{"missing", &ErrorValue{msg: "missing", expected: ";"}, `error("missing" expected ";")`},
		{"nested capture", func() any {
			n := newGNode(0, 3, newTuple(newTuple(newGNode(0, 1, "b"), newGNode(1, 2, "c")), newGNode(2, 3, "e")))
			n.src = &nodeSource{caps: []nodeCapture{{name: "y", path: []int{0, 0}}}}
			return n
		}(), `((y:"b" "c") "e")`},
		{"whole-value capture", func() any {
			n := newGNode(0, 1, newGNode(0, 1, "b"))
			n.src = &nodeSource{rule: "a", caps: []nodeCapture{{name: "x", path: []int{}}}}
			return n
		}(), `x:"b"@a`},
		{"lookahead capture", func() any {
			n := newGNode(0, 1, newTuple(newGNode(0, 1, "a")))
			n.src = &nodeSource{caps: []nodeCapture{{name: "e", side: true, value: newGNode(1, 1, newTuple())}}}
			return n
		}(), `("a" &e:())`},
		{"captures of an inner node stay inside it", func() any {
			inner := newGNode(0, 1, newTuple(newGNode(0, 1, "a")))
			inner.src = &nodeSource{rule: "in", caps: []nodeCapture{{name: "x", path: []int{0}}}}
			return newGNode(0, 1, newTuple(inner))
		}(), `((x:"a")@in)`},
	}
	for _, c := range cases {
		if got := valueString(c.v); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}

// TestValueWritersDeep checks that the writers do not recurse on deep trees.
func TestValueWritersDeep(t *testing.T) {
	var v any = "x"
	const depth = 200000
	for i := 0; i < depth; i++ {
		v = newGNode(0, 1, newTuple(v))
	}
	var b bytes.Buffer
	if err := writeValueJSON(&b, v); err != nil {
		t.Fatal(err)
	}
	// encoding/json rejects nesting deeper than 10000, so compare with the expected text.
	unit := `{"start":0,"end":1,"value":[`
	want := strings.Repeat(unit, depth) + `"x"` + strings.Repeat("]}", depth)
	if b.String() != want {
		t.Fatalf("deep JSON differs (length %d, want %d)", b.Len(), len(want))
	}
	if s := valueString(v); len(s) != 2*depth+3 {
		t.Fatalf("S-expression length %d", len(s))
	}
}
