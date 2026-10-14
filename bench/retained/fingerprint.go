package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"

	"github.com/ornew/pego"
)

const fingerprintFormat = "cst-value-graph-v1"

type frame string

// fingerprint visits every public node, field and value without recursively
// marshaling subtrees. Deterministic node IDs preserve sharing and node cycles.
// The benchmark corpora produce nodes, acyclic collections and scalar values.
func fingerprint(root *pego.Node) (string, error) {
	h := sha256.New()
	write := func(data []byte) {
		var size [binary.MaxVarintLen64]byte
		n := binary.PutUvarint(size[:], uint64(len(data)))
		h.Write(size[:n])
		h.Write(data)
	}
	write([]byte(fingerprintFormat))
	seen := map[*pego.Node]int{}
	stack := []any{root}
	for len(stack) != 0 {
		i := len(stack) - 1
		v := stack[i]
		stack[i] = nil
		stack = stack[:i]
		switch x := v.(type) {
		case frame:
			write([]byte(x))
			continue
		case *pego.Node:
			if x == nil {
				write([]byte("nil-node"))
				continue
			}
			if id, ok := seen[x]; ok {
				write([]byte(fmt.Sprintf("node-ref:%d", id)))
				continue
			}
			id := len(seen)
			seen[x] = id
			header := struct {
				ID                 int
				Type, Rule         string
				Start, End         int64
				Text               string
				Children, Fields   int
				NilKids, NilFields bool
				Terminal           bool
			}{id, x.Type(), x.Rule(), int64(x.Start), int64(x.End), x.Text, len(x.Children), len(x.Fields), x.Children == nil, x.Fields == nil, x.IsTerminal()}
			data, err := json.Marshal(header)
			if err != nil {
				return "", err
			}
			write([]byte("node"))
			write(data)
			fields := slices.Clone(x.Fields)
			sort.SliceStable(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
			for i := len(fields) - 1; i >= 0; i-- {
				stack = append(stack, fields[i].Value, frame("field:"+fields[i].Name))
			}
			for i := len(x.Children) - 1; i >= 0; i-- {
				stack = append(stack, x.Children[i])
			}
			continue
		}
		if v == nil {
			write([]byte("nil"))
			continue
		}
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Slice, reflect.Array:
			if rv.Kind() == reflect.Slice && rv.IsNil() {
				write([]byte("nil-slice:" + rv.Type().String()))
				continue
			}
			write([]byte(fmt.Sprintf("%s:%s:%d", rv.Kind(), rv.Type(), rv.Len())))
			for i := rv.Len() - 1; i >= 0; i-- {
				stack = append(stack, rv.Index(i).Interface())
			}
		case reflect.Map:
			if rv.Type().Key().Kind() != reflect.String {
				return "", fmt.Errorf("unsupported corpus map key %s", rv.Type().Key())
			}
			if rv.IsNil() {
				write([]byte("nil-map:" + rv.Type().String()))
				continue
			}
			keys := rv.MapKeys()
			sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
			write([]byte(fmt.Sprintf("map:%s:%d", rv.Type(), len(keys))))
			for i := len(keys) - 1; i >= 0; i-- {
				stack = append(stack, rv.MapIndex(keys[i]).Interface(), frame("key:"+keys[i].String()))
			}
		case reflect.Bool, reflect.String,
			reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
			data, err := json.Marshal(v)
			if err != nil {
				return "", err
			}
			write([]byte("scalar:" + rv.Type().String()))
			write(data)
		default:
			return "", fmt.Errorf("unsupported corpus value %T", v)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
