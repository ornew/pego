package engine

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
	caps   []captureLocation
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
func writeJSONRange(out *jsonOutput, n *Node, prefix string) error {
	if err := out.raw(prefix); err != nil {
		return err
	}
	if err := out.number(n.start); err != nil {
		return err
	}
	if err := out.raw(`,"end":`); err != nil {
		return err
	}
	return out.number(n.end)
}

type nodeJSONSliceID struct {
	ptr uintptr
	len int
}

// jsonOutput coalesces the small fragments of a node while preserving writer errors
// and the already-written prefix when an unsupported value is encountered.
type jsonOutput struct {
	w   io.Writer
	buf []byte
}

func (out *jsonOutput) flush() error {
	if len(out.buf) == 0 {
		return nil
	}
	if err := writeJSONBytes(out.w, out.buf); err != nil {
		return err
	}
	out.buf = out.buf[:0]
	return nil
}

func (out *jsonOutput) bytes(data []byte) error {
	if len(out.buf)+len(data) > cap(out.buf) {
		if err := out.flush(); err != nil {
			return err
		}
	}
	if len(data) > cap(out.buf) {
		return writeJSONBytes(out.w, data)
	}
	out.buf = append(out.buf, data...)
	return nil
}

func (out *jsonOutput) raw(s string) error {
	if len(out.buf)+len(s) > cap(out.buf) {
		if err := out.flush(); err != nil {
			return err
		}
	}
	out.buf = append(out.buf, s...)
	return nil
}

func (out *jsonOutput) number(n int32) error {
	if len(out.buf)+11 > cap(out.buf) {
		if err := out.flush(); err != nil {
			return err
		}
	}
	out.buf = strconv.AppendInt(out.buf, int64(n), 10)
	return nil
}

func (out *jsonOutput) encoded(value any) error {
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
	out := jsonOutput{w: w, buf: make([]byte, 0, 512)}
	stack := []nodeJSONFrame{{value: value}}
	activeNodes := make(map[*Node]struct{})
	activeSlices := make(map[nodeJSONSliceID]struct{})
	for len(stack) != 0 {
		frame := &stack[len(stack)-1]
		var err error
		switch value := frame.value.(type) {
		case *Node:
			if value == nil {
				err = out.raw("null")
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
				switch shape := value.shape(); shape {
				case shapeBox:
					// The box stands for its value.
					delete(activeNodes, value)
					stack[len(stack)-1] = nodeJSONFrame{value: value.box}
				case shapeString, shapeTuple, shapeList:
					prefix := `{"start":`
					if value.kind.nodeType() {
						// A node of a named node type names its type.
						if err = out.raw(`{"type":`); err == nil {
							err = out.encoded(value.Type())
						}
						prefix = `,"start":`
					}
					if err != nil {
						break
					}
					if err = writeJSONRange(&out, value, prefix); err != nil {
						break
					}
					if err = out.raw(`,"value":`); err != nil {
						break
					}
					if shape == shapeString {
						err = out.encoded(value.text)
						frame.phase = nodeJSONFieldsStart
						break
					}
					err = out.raw("[")
					frame.phase = nodeJSONChildren
				case shapeError:
					if err = writeJSONRange(&out, value, `{"start":`); err != nil {
						break
					}
					msg, _ := value.fields.Get("message")
					if err = out.raw(`,"error":`); err == nil {
						err = out.encoded(msg)
					}
					if expected, ok := value.fields.Get("expected"); ok && err == nil {
						if err = out.raw(`,"expected":`); err == nil {
							err = out.encoded(expected)
						}
					}
					frame.phase = nodeJSONExit
				case shapeTupleValue, shapeListValue:
					err = out.raw("[")
					frame.phase = nodeJSONSliceItems
				default: // a struct
					if err = out.raw(`{"type":`); err == nil {
						err = out.encoded(value.Type())
					}
					if err == nil {
						err = writeJSONRange(&out, value, `,"start":`)
					}
					if err == nil {
						err = out.raw(`,"fields":{`)
					}
					frame.fields = value.fields.sorted()
					frame.index = 0
					frame.phase = nodeJSONFields
				}
			case nodeJSONChildren, nodeJSONSliceItems:
				if frame.index < len(value.children) {
					if frame.index != 0 {
						if err = out.raw(","); err != nil {
							break
						}
					}
					child := value.children[frame.index]
					frame.index++
					stack = append(stack, nodeJSONFrame{value: child})
					break
				}
				if err = out.raw("]"); err != nil {
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
					if err = out.raw(`,"rule":`); err != nil {
						break
					}
					if err = out.encoded(r); err != nil {
						break
					}
				}
				frame.phase = nodeJSONExit
				if !value.labeled || len(value.fields) == 0 {
					break
				}
				frame.caps = captureLocations(value)
				frame.index = 0
				frame.phase = nodeJSONCaptures
				err = out.raw(`,"captures":{`)
			case nodeJSONCaptures:
				if frame.closeSide {
					frame.closeSide = false
					if err = out.raw("}"); err != nil {
						break
					}
				}
				if frame.index == len(frame.caps) {
					err = out.raw("}")
					frame.phase = nodeJSONExit
					break
				}
				c := frame.caps[frame.index]
				if frame.index != 0 {
					if err = out.raw(","); err != nil {
						break
					}
				}
				frame.index++
				if err = out.encoded(c.name); err != nil {
					break
				}
				if c.side {
					if err = out.raw(`:{"value":`); err != nil {
						break
					}
					frame.closeSide = true
					stack = append(stack, nodeJSONFrame{value: c.value})
					break
				}
				if err = out.raw(`:{"path":`); err == nil {
					err = out.encoded(c.path)
				}
				if err == nil {
					err = out.raw("}")
				}
			case nodeJSONFields:
				if frame.index < len(frame.fields) {
					if frame.index != 0 {
						if err = out.raw(","); err != nil {
							break
						}
					}
					field := frame.fields[frame.index]
					if err = out.encoded(field.Name); err != nil {
						break
					}
					if err = out.raw(":"); err != nil {
						break
					}
					frame.index++
					stack = append(stack, nodeJSONFrame{value: field.Value})
				} else {
					if err = out.raw("}"); err != nil {
						break
					}
					frame.phase = nodeJSONExit
				}
			case nodeJSONExit:
				delete(activeNodes, value)
				err = out.raw("}")
				stack = stack[:len(stack)-1]
			}
		case []any:
			if value == nil {
				err = out.raw("null")
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
				err = out.raw("[")
			} else if frame.index < len(value) {
				if frame.index != 0 {
					if err = out.raw(","); err != nil {
						break
					}
				}
				item := value[frame.index]
				frame.index++
				stack = append(stack, nodeJSONFrame{value: item})
			} else {
				delete(activeSlices, frame.slice)
				err = out.raw("]")
				stack = stack[:len(stack)-1]
			}
		default:
			err = out.encoded(value)
			stack = stack[:len(stack)-1]
		}
		if err != nil {
			if flushErr := out.flush(); flushErr != nil {
				return flushErr
			}
			return err
		}
	}
	return out.flush()
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
