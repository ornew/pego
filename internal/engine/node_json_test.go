package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"testing"
)

// referenceJSON builds the JSON shape of a value with encoding/json, without invoking
// Node.MarshalJSON: a string, tuple or list node is {start, end, value} (with rule when it has
// one), and a struct is {type, fields}. It is intentionally used only for shallow, acyclic test
// values without captures.
func referenceJSON(n *Node) any {
	if n == nil {
		return nil
	}
	type node struct {
		Start int32  `json:"start"`
		End   int32  `json:"end"`
		Value any    `json:"value"`
		Rule  string `json:"rule,omitempty"`
	}
	type structValue struct {
		Type   string         `json:"type"`
		Fields map[string]any `json:"fields"`
	}
	switch n.shape() {
	case shapeString:
		return node{n.Start, n.End, n.Text, n.Rule()}
	case shapeTuple, shapeList:
		elems := make([]any, len(n.Children))
		for i, c := range n.Children {
			elems[i] = referenceJSON(c)
		}
		return node{n.Start, n.End, elems, n.Rule()}
	}
	fields := make(map[string]any, len(n.Fields))
	for _, field := range n.Fields {
		fields[field.Name] = referenceField(field.Value)
	}
	return structValue{n.Type(), fields}
}

func referenceField(value any) any {
	switch value := value.(type) {
	case *Node:
		return referenceJSON(value)
	case []any:
		if value == nil {
			return []any(nil)
		}
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = referenceField(item)
		}
		return out
	default:
		return value
	}
}

func TestNodeWriteJSONMatchesReferenceShape(t *testing.T) {
	child := &Node{kind: kindMatch, Start: 3, End: 5, Text: "<&>\xff"}
	record := &Node{
		kind:  kindOf("Record", "entry"),
		Start: 1,
		End:   8,
		Fields: Fields{
			{Name: "z", Value: []any{"<tag>", []byte{0, 1, 255}, child}},
			{Name: "a", Value: int64(9007199254740992)},
			{Name: "nilList", Value: []any(nil)},
			{Name: "emptyList", Value: []any{}},
		},
	}
	tuple := &Node{kind: kindOf("Seq", "pair"), Start: 1, End: 8, Children: []*Node{child, nil, child}}
	for _, n := range []*Node{record, tuple} {
		want, err := json.Marshal(referenceJSON(n))
		if err != nil {
			t.Fatal(err)
		}
		got, err := n.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("MarshalJSON mismatch\n got: %s\nwant: %s", got, want)
		}
		var streamed bytes.Buffer
		if err := n.WriteJSON(&streamed); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(streamed.Bytes(), want) {
			t.Fatalf("WriteJSON mismatch\n got: %s\nwant: %s", streamed.Bytes(), want)
		}
	}
}

func TestNodeWriteJSONNilAndFields(t *testing.T) {
	var n *Node
	got, err := n.MarshalJSON()
	if err != nil || string(got) != "null" {
		t.Fatalf("nil MarshalJSON = %q, %v; want null, nil", got, err)
	}
	data, err := json.Marshal(n)
	if err != nil || string(data) != "null" {
		t.Fatalf("json.Marshal nil Node = %q, %v; want null, nil", data, err)
	}
	var streamed bytes.Buffer
	if err := n.WriteJSON(&streamed); err != nil || streamed.String() != "null" {
		t.Fatalf("nil WriteJSON = %q, %v; want null, nil", streamed.String(), err)
	}

	fields := Fields{{Name: "child", Value: &Node{kind: kindMatch, Text: "x"}}, {Name: "bytes", Value: []byte{1, 2}}}
	got, err = fields.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"bytes":"AQI=","child":{"start":0,"end":0,"value":"x"}}`; string(got) != want {
		t.Fatalf("Fields.MarshalJSON = %s, want %s", got, want)
	}
	listFields := Fields{{Name: "nil", Value: []any(nil)}, {Name: "empty", Value: []any{}}}
	got, err = listFields.MarshalJSON()
	if err != nil || string(got) != `{"empty":[],"nil":null}` {
		t.Fatalf("Fields nil/empty list JSON = %s, %v; want {\"empty\":[],\"nil\":null}", got, err)
	}
}

func TestNodeWriteJSONSharedNodesAndCycles(t *testing.T) {
	leaf := &Node{kind: kindMatch, Text: "shared"}
	root := &Node{kind: kindSeq, Children: []*Node{leaf, leaf}}
	data, err := root.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(data, []byte(`"value":"shared"`)); got != 2 {
		t.Fatalf("shared node appeared %d times in %s, want 2", got, data)
	}

	for _, makeCycle := range []struct {
		name string
		make func() *Node
	}{
		{"child", func() *Node { n := &Node{kind: kindSeq}; n.Children = []*Node{n}; return n }},
		{"field", func() *Node { n := &Node{kind: kindOf("R", "")}; n.Fields = Fields{{Name: "self", Value: n}}; return n }},
		{"slice", func() *Node {
			var values []any
			values = append(values, nil)
			values[0] = values
			return &Node{kind: kindOf("R", ""), Fields: Fields{{Name: "cycle", Value: values}}}
		}},
	} {
		t.Run(makeCycle.name, func(t *testing.T) {
			_, err := makeCycle.make().MarshalJSON()
			var cycle *json.UnsupportedValueError
			if !errors.As(err, &cycle) {
				t.Fatalf("MarshalJSON error = %T %v, want *json.UnsupportedValueError", err, err)
			}
		})
	}
}

type sentinelJSONError struct{ cause error }

func (e sentinelJSONError) Error() string { return "sentinel marshal failure" }
func (e sentinelJSONError) Unwrap() error { return e.cause }

type failingJSONValue struct{ err error }

func (v failingJSONValue) MarshalJSON() ([]byte, error) { return nil, sentinelJSONError{v.err} }

func TestNodeWriteJSONPreservesLeafErrors(t *testing.T) {
	want := errors.New("leaf cause")
	n := &Node{kind: kindOf("R", ""), Fields: Fields{{Name: "bad", Value: failingJSONValue{want}}}}
	for name, err := range map[string]error{
		"WriteJSON":      n.WriteJSON(io.Discard),
		"MarshalJSON":    func() error { _, err := n.MarshalJSON(); return err }(),
		"json.Marshal":   func() error { _, err := json.Marshal(n); return err }(),
		"Fields.Marshal": func() error { _, err := n.Fields.MarshalJSON(); return err }(),
	} {
		var marshalerErr *json.MarshalerError
		var sentinel sentinelJSONError
		if !errors.As(err, &marshalerErr) || !errors.As(err, &sentinel) || !errors.Is(err, want) {
			t.Errorf("%s error chain does not preserve leaf error: %T %v", name, err, err)
		}
	}
}

func TestNodeWriteJSONDeepAndStandardLibraryLimit(t *testing.T) {
	const depth = 12_000
	root := &Node{kind: kindSeq}
	current := root
	for i := 1; i < depth; i++ {
		child := &Node{kind: kindSeq}
		current.Children = []*Node{child}
		current = child
	}
	var sink countingWriter
	if err := root.WriteJSON(&sink); err != nil {
		t.Fatal(err)
	}
	if sink.n < depth*20 {
		t.Fatalf("WriteJSON wrote only %d bytes for %d nodes", sink.n, depth)
	}
	if _, err := root.MarshalJSON(); err != nil {
		t.Fatalf("direct MarshalJSON failed at depth %d: %v", depth, err)
	}
	if _, err := json.Marshal(root); err == nil {
		t.Fatalf("encoding/json unexpectedly accepted %d nested nodes; its output scanner limit remains a boundary", depth)
	} else {
		var marshalerErr *json.MarshalerError
		var syntaxErr *json.SyntaxError
		if !errors.As(err, &marshalerErr) || !errors.As(err, &syntaxErr) {
			t.Fatalf("json.Marshal deep-node error = %T %v, want MarshalerError wrapping the standard scanner error", err, err)
		}
	}
}

type countingWriter struct{ n int }

func (w *countingWriter) Write(p []byte) (int, error) { w.n += len(p); return len(p), nil }

func TestNodeWriteJSONShortWriter(t *testing.T) {
	n := &Node{kind: kindMatch, Text: "x"}
	err := n.WriteJSON(shortWriter{})
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("WriteJSON short write error = %v, want io.ErrShortWrite", err)
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return len(p) - 1, nil
}

func TestNodeWriteJSONLargeScalarAcrossBufferBoundary(t *testing.T) {
	n := &Node{kind: kindMatch, Text: strings.Repeat("x", 600)}
	want, err := json.Marshal(referenceJSON(n))
	if err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	if err := n.WriteJSON(&got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("large scalar JSON mismatch\n got: %s\nwant: %s", got.Bytes(), want)
	}
}

type partialAfterFlushWriter struct {
	bytes.Buffer
	calls int
	err   error
}

func (w *partialAfterFlushWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == 1 {
		return w.Buffer.Write(p)
	}
	n := len(p) / 2
	w.Buffer.Write(p[:n])
	return n, w.err
}

func TestNodeWriteJSONPartialWriteErrorAfterFlush(t *testing.T) {
	n := &Node{kind: kindMatch, Text: strings.Repeat("x", 600)}
	wantErr := errors.New("writer failed")
	w := &partialAfterFlushWriter{err: wantErr}
	err := n.WriteJSON(w)
	if !errors.Is(err, wantErr) {
		t.Fatalf("WriteJSON error = %v, want writer error", err)
	}
	if w.calls != 2 {
		t.Fatalf("writer calls = %d, want prefix flush and partial scalar write", w.calls)
	}
	encoded, err := json.Marshal(n.Text)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := `{"start":0,"end":0,"value":` + string(encoded[:len(encoded)/2])
	if w.String() != wantPrefix {
		t.Fatalf("written prefix = %q, want %q", w.String(), wantPrefix)
	}
}

func TestNodeWriteJSONInvalidValueParity(t *testing.T) {
	n := &Node{kind: kindOf("R", ""), Fields: Fields{{Name: "bad", Value: math.NaN()}}}
	_, gotErr := n.MarshalJSON()
	_, wantErr := json.Marshal(struct {
		Type   string         `json:"type"`
		Start  int32          `json:"start"`
		End    int32          `json:"end"`
		Fields map[string]any `json:"fields,omitempty"`
	}{Type: "Seq", Fields: map[string]any{"bad": math.NaN()}})
	var gotUnsupported, wantUnsupported *json.UnsupportedValueError
	if !errors.As(gotErr, &gotUnsupported) || !errors.As(wantErr, &wantUnsupported) || gotUnsupported.Str != wantUnsupported.Str {
		t.Fatalf("unsupported-value errors differ: got %T %v, want %T %v", gotErr, gotErr, wantErr, wantErr)
	}
}

func TestNodeJSONSliceIdentityAllowsRepeatedAcyclicEdges(t *testing.T) {
	shared := []any{"ok"}
	n := &Node{kind: kindOf("R", ""), Fields: Fields{{Name: "a", Value: shared}, {Name: "b", Value: shared}}}
	got, err := n.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"a":["ok"],"b":["ok"]`) {
		t.Fatalf("shared acyclic list did not serialize at both edges: %s", got)
	}
}

func TestNodeWriteJSONSliceIdentityIgnoresCapacity(t *testing.T) {
	x := make([]any, 1, 2)
	x[0] = x[:1:1]
	root := &Node{Fields: Fields{{Name: "x", Value: x}}}
	var prefix bytes.Buffer
	err := root.WriteJSON(&prefix)
	var cycle *json.UnsupportedValueError
	if !errors.As(err, &cycle) {
		t.Fatalf("capacity-limited slice cycle = %v, want UnsupportedValueError", err)
	}
	// Reject the first re-entry, before emitting another list delimiter.
	const wantPrefix = `{"type":"","fields":{"x":[`
	if prefix.String() != wantPrefix {
		t.Fatalf("cycle prefix = %q, want %q", prefix.String(), wantPrefix)
	}
	if data, err := root.MarshalJSON(); data != nil || !errors.As(err, &cycle) {
		t.Fatalf("MarshalJSON cycle = %q, %v, want nil bytes and UnsupportedValueError", data, err)
	}
}
