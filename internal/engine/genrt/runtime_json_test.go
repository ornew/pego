package genrt

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"testing"
)

func TestNodeWriteJSONShapeAndNil(t *testing.T) {
	child := &Node{kind: kindMatch, Start: 2, End: 3, Text: "<&>"}
	n := &Node{
		kind:     kindSeq,
		Children: []*Node{child, nil, child},
		Fields:   Fields{{Name: "z", Value: []any{"x", []byte{1, 2}}}, {Name: "a", Value: child}, {Name: "nilList", Value: []any(nil)}, {Name: "emptyList", Value: []any{}}},
	}
	want := `{"type":"Seq","start":0,"end":0,"children":[{"type":"Match","start":2,"end":3,"text":"\u003c\u0026\u003e"},null,{"type":"Match","start":2,"end":3,"text":"\u003c\u0026\u003e"}],"fields":{"a":{"type":"Match","start":2,"end":3,"text":"\u003c\u0026\u003e"},"emptyList":[],"nilList":null,"z":["x","AQI="]}}`
	got, err := n.MarshalJSON()
	if err != nil || string(got) != want {
		t.Fatalf("MarshalJSON = %s, %v; want %s", got, err, want)
	}
	var streamed bytes.Buffer
	if err := n.WriteJSON(&streamed); err != nil || streamed.String() != want {
		t.Fatalf("WriteJSON = %s, %v; want %s", streamed.String(), err, want)
	}
	var nilNode *Node
	got, err = nilNode.MarshalJSON()
	if err != nil || string(got) != "null" {
		t.Fatalf("nil MarshalJSON = %s, %v; want null", got, err)
	}
	got, err = json.Marshal(nilNode)
	if err != nil || string(got) != "null" {
		t.Fatalf("json.Marshal(nil Node) = %s, %v; want null", got, err)
	}
	fields := Fields{{Name: "nil", Value: []any(nil)}, {Name: "empty", Value: []any{}}}
	got, err = fields.MarshalJSON()
	if err != nil || string(got) != `{"empty":[],"nil":null}` {
		t.Fatalf("Fields nil/empty list JSON = %s, %v; want {\"empty\":[],\"nil\":null}", got, err)
	}
}

func TestNodeWriteJSONCyclesAndSharedEdges(t *testing.T) {
	leaf := &Node{kind: kindMatch, Text: "shared"}
	root := &Node{kind: kindSeq, Children: []*Node{leaf, leaf}}
	data, err := root.MarshalJSON()
	if err != nil || bytes.Count(data, []byte(`"text":"shared"`)) != 2 {
		t.Fatalf("shared DAG output = %s, %v", data, err)
	}
	root.Children = []*Node{root}
	_, err = root.MarshalJSON()
	var cycle *json.UnsupportedValueError
	if !errors.As(err, &cycle) {
		t.Fatalf("cycle error = %T %v, want *json.UnsupportedValueError", err, err)
	}
	var list []any
	list = append(list, nil)
	list[0] = list
	root.Children = nil
	root.Fields = Fields{{Name: "loop", Value: list}}
	_, err = root.MarshalJSON()
	if !errors.As(err, &cycle) {
		t.Fatalf("nested []any cycle error = %T %v, want *json.UnsupportedValueError", err, err)
	}
}

func TestNodeWriteJSONDeep(t *testing.T) {
	const depth = 12_000
	root := &Node{kind: kindSeq}
	current := root
	for i := 1; i < depth; i++ {
		child := &Node{kind: kindSeq}
		current.Children = []*Node{child}
		current = child
	}
	var out bytes.Buffer
	if err := root.WriteJSON(&out); err != nil {
		t.Fatal(err)
	}
	if _, err := root.MarshalJSON(); err != nil {
		t.Fatal(err)
	}
}

type runtimeJSONFailure struct{ cause error }

func (e runtimeJSONFailure) Error() string { return "runtime JSON failure" }
func (e runtimeJSONFailure) Unwrap() error { return e.cause }

type runtimeJSONValue struct{ cause error }

func (v runtimeJSONValue) MarshalJSON() ([]byte, error) {
	return nil, runtimeJSONFailure{cause: v.cause}
}

func TestNodeWriteJSONPreservesLeafError(t *testing.T) {
	want := errors.New("underlying")
	n := &Node{kind: kindSeq, Fields: Fields{{Name: "value", Value: runtimeJSONValue{cause: want}}}}
	for name, call := range map[string]func() error{
		"WriteJSON": func() error { return n.WriteJSON(io.Discard) },
		"MarshalJSON": func() error {
			_, err := n.MarshalJSON()
			return err
		},
		"json.Marshal": func() error {
			_, err := json.Marshal(n)
			return err
		},
	} {
		err := call()
		var failure runtimeJSONFailure
		if !errors.As(err, &failure) || !errors.Is(err, want) {
			t.Errorf("%s did not preserve underlying error: %T %v", name, err, err)
		}
	}
}
