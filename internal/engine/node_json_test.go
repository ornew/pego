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

type legacyNodeJSON struct {
	Type     string            `json:"type"`
	Rule     string            `json:"rule,omitempty"`
	Start    int32             `json:"start"`
	End      int32             `json:"end"`
	Text     string            `json:"text,omitempty"`
	Children []*legacyNodeJSON `json:"children,omitempty"`
	Fields   map[string]any    `json:"fields,omitempty"`
}

// legacyNodeReference builds the old nodeJSON/encoding/json shape without invoking Node.MarshalJSON.
// It is intentionally used only for shallow, acyclic test values.
func legacyNodeReference(n *Node) *legacyNodeJSON {
	if n == nil {
		return nil
	}
	out := &legacyNodeJSON{Type: n.Type(), Rule: n.Rule(), Start: n.Start, End: n.End, Text: n.Text}
	if len(n.Children) != 0 {
		out.Children = make([]*legacyNodeJSON, len(n.Children))
		for i, child := range n.Children {
			out.Children[i] = legacyNodeReference(child)
		}
	}
	if len(n.Fields) != 0 {
		out.Fields = make(map[string]any, len(n.Fields))
		for _, field := range n.Fields {
			out.Fields[field.Name] = legacyFieldReference(field.Value)
		}
	}
	return out
}

func legacyFieldReference(value any) any {
	switch value := value.(type) {
	case *Node:
		return legacyNodeReference(value)
	case []any:
		if value == nil {
			return []any(nil)
		}
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = legacyFieldReference(item)
		}
		return out
	default:
		return value
	}
}

func TestNodeWriteJSONMatchesLegacyShape(t *testing.T) {
	child := &Node{kind: kindMatch, Start: 3, End: 5, Text: "<&>\xff"}
	n := &Node{
		kind:     kindOf("Record", "entry"),
		Start:    1,
		End:      8,
		Children: []*Node{child, nil, child},
		Fields: Fields{
			{Name: "z", Value: []any{"<tag>", []byte{0, 1, 255}, child}},
			{Name: "a", Value: int64(9007199254740992)},
			{Name: "nilList", Value: []any(nil)},
			{Name: "emptyList", Value: []any{}},
		},
	}
	want, err := json.Marshal(legacyNodeReference(n))
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
	if want := `{"bytes":"AQI=","child":{"type":"Match","start":0,"end":0,"text":"x"}}`; string(got) != want {
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
	if got := bytes.Count(data, []byte(`"text":"shared"`)); got != 2 {
		t.Fatalf("shared node appeared %d times in %s, want 2", got, data)
	}

	for _, makeCycle := range []struct {
		name string
		make func() *Node
	}{
		{"child", func() *Node { n := &Node{kind: kindSeq}; n.Children = []*Node{n}; return n }},
		{"field", func() *Node { n := &Node{kind: kindSeq}; n.Fields = Fields{{Name: "self", Value: n}}; return n }},
		{"slice", func() *Node {
			var values []any
			values = append(values, nil)
			values[0] = values
			return &Node{kind: kindSeq, Fields: Fields{{Name: "cycle", Value: values}}}
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
	n := &Node{kind: kindSeq, Fields: Fields{{Name: "bad", Value: failingJSONValue{want}}}}
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

func TestNodeWriteJSONInvalidValueParity(t *testing.T) {
	n := &Node{kind: kindSeq, Fields: Fields{{Name: "bad", Value: math.NaN()}}}
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
	n := &Node{kind: kindSeq, Fields: Fields{{Name: "a", Value: shared}, {Name: "b", Value: shared}}}
	got, err := n.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"a":["ok"],"b":["ok"]`) {
		t.Fatalf("shared acyclic list did not serialize at both edges: %s", got)
	}
}
