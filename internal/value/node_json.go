package value

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
)

type nodeJSONFrame struct {
	value  any
	fields Fields
	caps   []CaptureLocation
	index  int
	phase  uint8
	slice  nodeJSONSliceID
	// closeSide reports that the value of a capture beside the tuple was written and its object
	// needs closing.
	closeSide bool
}

const (
	nodeJSONEnter uint8 = iota
	nodeJSONChildren
	nodeJSONFieldsStart
	nodeJSONFields
	nodeJSONExit
	nodeJSONSliceItems
	nodeJSONCaptures
)

// writeJSONRange writes prefix and the members start and end of n.
func writeJSONRange(out *JSONOutput, n *Node, prefix string) error {
	if err := out.Raw(prefix); err != nil {
		return err
	}
	if err := out.Number(n.d.Start); err != nil {
		return err
	}
	if err := out.Raw(`,"end":`); err != nil {
		return err
	}
	return out.Number(n.d.End)
}

type nodeJSONSliceID struct {
	ptr uintptr
	len int
}

// JSONOutput coalesces the small fragments of a node while preserving writer errors
// and the already-written prefix when an unsupported value is encountered.
type JSONOutput struct {
	W   io.Writer
	Buf []byte
}

func (out *JSONOutput) Flush() error {
	if len(out.Buf) == 0 {
		return nil
	}
	if err := writeJSONBytes(out.W, out.Buf); err != nil {
		return err
	}
	out.Buf = out.Buf[:0]
	return nil
}

func (out *JSONOutput) bytes(data []byte) error {
	if len(out.Buf)+len(data) > cap(out.Buf) {
		if err := out.Flush(); err != nil {
			return err
		}
	}
	if len(data) > cap(out.Buf) {
		return writeJSONBytes(out.W, data)
	}
	out.Buf = append(out.Buf, data...)
	return nil
}

func (out *JSONOutput) Raw(s string) error {
	if len(out.Buf)+len(s) > cap(out.Buf) {
		if err := out.Flush(); err != nil {
			return err
		}
	}
	out.Buf = append(out.Buf, s...)
	return nil
}

func (out *JSONOutput) Number(n int32) error {
	if len(out.Buf)+11 > cap(out.Buf) {
		if err := out.Flush(); err != nil {
			return err
		}
	}
	out.Buf = strconv.AppendInt(out.Buf, int64(n), 10)
	return nil
}

func (out *JSONOutput) Encoded(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return out.bytes(data)
}

// writeJSONValue encodes PEGO nodes and []any values without recursive calls through their nested
// edges. Other values are delegated to encoding/json, preserving its scalar and custom-marshaler
// behavior. []byte intentionally stays in that fallback so it keeps encoding as base64.
func writeJSONValue(w io.Writer, value any) error {
	out := JSONOutput{W: w, Buf: make([]byte, 0, 512)}
	stack := []nodeJSONFrame{{value: value}}
	activeNodes := make(map[*Node]struct{})
	activeSlices := make(map[nodeJSONSliceID]struct{})
	for len(stack) != 0 {
		frame := &stack[len(stack)-1]
		var err error
		switch value := frame.value.(type) {
		case *Node:
			if value == nil {
				err = out.Raw("null")
				stack = stack[:len(stack)-1]
				break
			}
			switch frame.phase {
			case nodeJSONEnter:
				if _, ok := activeNodes[value]; ok {
					err = jsonCycleError(value)
					break
				}
				activeNodes[value] = struct{}{}
				switch shape := ShapeOf(value); shape {
				case ShapeBox:
					// The box stands for its value.
					delete(activeNodes, value)
					stack[len(stack)-1] = nodeJSONFrame{value: value.d.Box}
				case ShapeString, ShapeTuple, ShapeList:
					prefix := `{"start":`
					if value.d.Kind.IsNodeType() {
						// A node of a named node type names its type.
						if err = out.Raw(`{"type":`); err == nil {
							err = out.Encoded(value.Type())
						}
						prefix = `,"start":`
					}
					if err != nil {
						break
					}
					if err = writeJSONRange(&out, value, prefix); err != nil {
						break
					}
					if err = out.Raw(`,"value":`); err != nil {
						break
					}
					if shape == ShapeString {
						err = out.Encoded(value.d.Text)
						frame.phase = nodeJSONFieldsStart
						break
					}
					err = out.Raw("[")
					frame.phase = nodeJSONChildren
				case ShapeError:
					if err = writeJSONRange(&out, value, `{"start":`); err != nil {
						break
					}
					msg, _ := value.d.Fields.Get("message")
					if err = out.Raw(`,"error":`); err == nil {
						err = out.Encoded(msg)
					}
					if expected, ok := value.d.Fields.Get("expected"); ok && err == nil {
						if err = out.Raw(`,"expected":`); err == nil {
							err = out.Encoded(expected)
						}
					}
					frame.phase = nodeJSONExit
				case ShapeTupleValue, ShapeListValue:
					err = out.Raw("[")
					frame.phase = nodeJSONSliceItems
				default: // a struct
					if err = out.Raw(`{"type":`); err == nil {
						err = out.Encoded(value.Type())
					}
					if err == nil {
						err = writeJSONRange(&out, value, `,"start":`)
					}
					if err == nil {
						err = out.Raw(`,"fields":{`)
					}
					frame.fields = value.d.Fields.sorted()
					frame.index = 0
					frame.phase = nodeJSONFields
				}
			case nodeJSONChildren, nodeJSONSliceItems:
				if frame.index < len(value.d.Children) {
					if frame.index != 0 {
						if err = out.Raw(","); err != nil {
							break
						}
					}
					child := value.d.Children[frame.index]
					frame.index++
					stack = append(stack, nodeJSONFrame{value: child})
					break
				}
				if err = out.Raw("]"); err != nil {
					break
				}
				if frame.phase == nodeJSONSliceItems {
					delete(activeNodes, value)
					stack = stack[:len(stack)-1]
					break
				}
				frame.phase = nodeJSONFieldsStart
			case nodeJSONFieldsStart:
				// Source information: the rule, and where the captures are.
				if r := value.Rule(); r != "" {
					if err = out.Raw(`,"rule":`); err != nil {
						break
					}
					if err = out.Encoded(r); err != nil {
						break
					}
				}
				frame.phase = nodeJSONExit
				if !value.d.Labeled || len(value.d.Fields) == 0 {
					break
				}
				frame.caps = CaptureLocations(value)
				frame.index = 0
				frame.phase = nodeJSONCaptures
				err = out.Raw(`,"captures":{`)
			case nodeJSONCaptures:
				if frame.closeSide {
					frame.closeSide = false
					if err = out.Raw("}"); err != nil {
						break
					}
				}
				if frame.index == len(frame.caps) {
					err = out.Raw("}")
					frame.phase = nodeJSONExit
					break
				}
				c := frame.caps[frame.index]
				if frame.index != 0 {
					if err = out.Raw(","); err != nil {
						break
					}
				}
				frame.index++
				if err = out.Encoded(c.Name); err != nil {
					break
				}
				if c.Side {
					if err = out.Raw(`:{"value":`); err != nil {
						break
					}
					frame.closeSide = true
					stack = append(stack, nodeJSONFrame{value: c.Value})
					break
				}
				if err = out.Raw(`:{"path":`); err == nil {
					err = out.Encoded(c.Path)
				}
				if err == nil {
					err = out.Raw("}")
				}
			case nodeJSONFields:
				if frame.index < len(frame.fields) {
					if frame.index != 0 {
						if err = out.Raw(","); err != nil {
							break
						}
					}
					field := frame.fields[frame.index]
					if err = out.Encoded(field.Name); err != nil {
						break
					}
					if err = out.Raw(":"); err != nil {
						break
					}
					frame.index++
					stack = append(stack, nodeJSONFrame{value: field.Value})
				} else {
					if err = out.Raw("}"); err != nil {
						break
					}
					frame.phase = nodeJSONExit
				}
			case nodeJSONExit:
				delete(activeNodes, value)
				err = out.Raw("}")
				stack = stack[:len(stack)-1]
			}
		case []any:
			if value == nil {
				err = out.Raw("null")
				stack = stack[:len(stack)-1]
				break
			}
			if frame.phase == nodeJSONEnter {
				id := sliceIdentity(value)
				if _, ok := activeSlices[id]; ok {
					err = jsonCycleError(value)
					break
				}
				activeSlices[id] = struct{}{}
				frame.slice = id
				frame.phase = nodeJSONSliceItems
				err = out.Raw("[")
			} else if frame.index < len(value) {
				if frame.index != 0 {
					if err = out.Raw(","); err != nil {
						break
					}
				}
				item := value[frame.index]
				frame.index++
				stack = append(stack, nodeJSONFrame{value: item})
			} else {
				delete(activeSlices, frame.slice)
				err = out.Raw("]")
				stack = stack[:len(stack)-1]
			}
		default:
			err = out.Encoded(value)
			stack = stack[:len(stack)-1]
		}
		if err != nil {
			if flushErr := out.Flush(); flushErr != nil {
				return flushErr
			}
			return err
		}
	}
	return out.Flush()
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

// WriteJSON writes the live node graph as JSON without recursive Node or []any traversal.
// A nil receiver writes null. Shared edges are encoded separately; active-path cycles fail.
// It does not close or flush w, and an error may leave a prefix written. Maps and other
// containers delegate to encoding/json and are outside the iterative depth guarantee.
// Callers must not mutate reachable values concurrently with this method.
func (n *Node) WriteJSON(w io.Writer) error {
	return writeJSONValue(w, n)
}

func sliceIdentity(value []any) nodeJSONSliceID {
	return nodeJSONSliceID{ptr: reflect.ValueOf(value).Pointer(), len: len(value)}
}
