package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	"github.com/ornew/zeroparse"
)

const fingerprintFormat = "cst-value-graph-v2"

type frame string

type field struct {
	name  string
	value any
}

// fingerprint visits every public node, field and value without recursively
// marshaling subtrees. Deterministic node IDs preserve sharing and node cycles.
// The benchmark corpora produce nodes, acyclic collections and scalar values.
func fingerprint(root *zp.Node) (string, error) {
	h := sha256.New()
	write := func(data []byte) {
		var size [binary.MaxVarintLen64]byte
		n := binary.PutUvarint(size[:], uint64(len(data)))
		h.Write(size[:n])
		h.Write(data)
	}
	write([]byte(fingerprintFormat))
	seen := map[*zp.Node]int{}
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
		case *zp.Node:
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
			var fields []field
			for name, value := range x.Fields() {
				fields = append(fields, field{name, value})
			}
			header := struct {
				ID         int
				Kind       string
				Type, Rule string
				Start, End int64
				Text       string
				Elems      int
				Fields     int
				Terminal   bool
			}{id, x.Kind().String(), x.Type(), x.Rule(), int64(x.Start()), int64(x.End()), x.Text(), x.Len(), len(fields), x.IsTerminal()}
			data, err := json.Marshal(header)
			if err != nil {
				return "", err
			}
			write([]byte("node"))
			write(data)
			sort.SliceStable(fields, func(i, j int) bool { return fields[i].name < fields[j].name })
			for i := len(fields) - 1; i >= 0; i-- {
				stack = append(stack, fields[i].value, frame("field:"+fields[i].name))
			}
			for i := x.Len() - 1; i >= 0; i-- {
				stack = append(stack, x.Elem(i))
			}
			if x.Kind() == zp.KindValue {
				stack = append(stack, x.Value())
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
