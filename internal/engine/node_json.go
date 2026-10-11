package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
)

type nodeJSONTaskKind uint8

const (
	nodeJSONEmit nodeJSONTaskKind = iota
	nodeJSONExitNode
	nodeJSONExitSlice
	nodeJSONRaw
)

type nodeJSONTask struct {
	kind  nodeJSONTaskKind
	value any
}

type nodeJSONSliceID struct {
	ptr uintptr
	len int
	cap int
}

// writeJSONValue encodes PEGO nodes and []any values without recursive calls through their nested
// edges. Other values are delegated to encoding/json, preserving its scalar and custom-marshaler
// behavior. []byte intentionally stays in that fallback so it keeps encoding as base64.
func writeJSONValue(w io.Writer, value any) error {
	stack := []nodeJSONTask{{kind: nodeJSONEmit, value: value}}
	activeNodes := make(map[*Node]struct{})
	activeSlices := make(map[nodeJSONSliceID]struct{})
	for len(stack) != 0 {
		task := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch task.kind {
		case nodeJSONRaw:
			if err := writeJSONBytes(w, []byte(task.value.(string))); err != nil {
				return err
			}
			continue
		case nodeJSONExitNode:
			delete(activeNodes, task.value.(*Node))
			continue
		case nodeJSONExitSlice:
			delete(activeSlices, task.value.(nodeJSONSliceID))
			continue
		}

		switch value := task.value.(type) {
		case *Node:
			if value == nil {
				if err := writeJSONBytes(w, []byte("null")); err != nil {
					return err
				}
				continue
			}
			if _, ok := activeNodes[value]; ok {
				return jsonCycleError(value)
			}
			activeNodes[value] = struct{}{}
			stack = append(stack, nodeJSONTask{kind: nodeJSONExitNode, value: value})
			parts := nodeJSONParts(value)
			for i := len(parts) - 1; i >= 0; i-- {
				stack = append(stack, parts[i])
			}
		case []any:
			if value == nil {
				if err := writeJSONBytes(w, []byte("null")); err != nil {
					return err
				}
				continue
			}
			id := nodeJSONSliceID{ptr: reflect.ValueOf(value).Pointer(), len: len(value), cap: cap(value)}
			if _, ok := activeSlices[id]; ok {
				return jsonCycleError(value)
			}
			activeSlices[id] = struct{}{}
			stack = append(stack, nodeJSONTask{kind: nodeJSONRaw, value: "]"})
			stack = append(stack, nodeJSONTask{kind: nodeJSONExitSlice, value: id})
			for i := len(value) - 1; i >= 0; i-- {
				stack = append(stack, nodeJSONTask{kind: nodeJSONEmit, value: value[i]})
				if i > 0 {
					stack = append(stack, nodeJSONTask{kind: nodeJSONRaw, value: ","})
				}
			}
			stack = append(stack, nodeJSONTask{kind: nodeJSONRaw, value: "["})
		default:
			data, err := json.Marshal(value)
			if err != nil {
				return err
			}
			if err := writeJSONBytes(w, data); err != nil {
				return err
			}
		}
	}
	return nil
}

func nodeJSONParts(n *Node) []nodeJSONTask {
	parts := []nodeJSONTask{
		nodeJSONRawTask(`{"type":`),
		nodeJSONValueTask(n.Type()),
	}
	if n.Rule() != "" {
		parts = append(parts,
			nodeJSONRawTask(`,"rule":`),
			nodeJSONValueTask(n.Rule()),
		)
	}
	parts = append(parts,
		nodeJSONRawTask(`,"start":`),
		nodeJSONValueTask(n.Start),
		nodeJSONRawTask(`,"end":`),
		nodeJSONValueTask(n.End),
	)
	if n.Text != "" {
		parts = append(parts,
			nodeJSONRawTask(`,"text":`),
			nodeJSONValueTask(n.Text),
		)
	}
	if len(n.Children) != 0 {
		parts = append(parts, nodeJSONRawTask(`,"children":[`))
		for i, child := range n.Children {
			if i != 0 {
				parts = append(parts, nodeJSONRawTask(","))
			}
			parts = append(parts, nodeJSONValueTask(child))
		}
		parts = append(parts, nodeJSONRawTask("]"))
	}
	if len(n.Fields) != 0 {
		parts = append(parts, nodeJSONRawTask(`,"fields":{`))
		for i, field := range n.Fields.sorted() {
			if i != 0 {
				parts = append(parts, nodeJSONRawTask(","))
			}
			parts = append(parts,
				nodeJSONValueTask(field.Name),
				nodeJSONRawTask(":"),
				nodeJSONValueTask(field.Value),
			)
		}
		parts = append(parts, nodeJSONRawTask("}"))
	}
	parts = append(parts, nodeJSONRawTask("}"))
	return parts
}

func nodeJSONRawTask(value string) nodeJSONTask {
	return nodeJSONTask{kind: nodeJSONRaw, value: value}
}

func nodeJSONValueTask(value any) nodeJSONTask {
	return nodeJSONTask{kind: nodeJSONEmit, value: value}
}

func jsonCycleError(value any) error {
	t := reflect.TypeOf(value)
	return &json.UnsupportedValueError{Value: reflect.ValueOf(value), Str: fmt.Sprintf("encountered a cycle via %s", t)}
}

func writeJSONBytes(w io.Writer, data []byte) error {
	n, err := w.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}
