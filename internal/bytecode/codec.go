package bytecode

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
)

// Encoder writes the payload of a compiled grammar: variable-length integers and strings by their
// index in a string table, which Finish writes before the body.
type Encoder struct {
	Body bytes.Buffer   // the encoded values
	Strs map[string]int // index of each string in List
	List []string       // the string table
}

// Byte writes b.
func (w *Encoder) Byte(b byte) { w.Body.WriteByte(b) }

// Uint writes the non-negative integer n.
func (w *Encoder) Uint(n int) {
	w.Body.Write(binary.AppendUvarint(nil, uint64(n)))
}

// Int writes the signed integer n, zigzag-encoded.
func (w *Encoder) Int(n int) {
	w.Body.Write(binary.AppendVarint(nil, int64(n)))
}

// Bool writes b as one byte.
func (w *Encoder) Bool(b bool) {
	if b {
		w.Byte(1)
	} else {
		w.Byte(0)
	}
}

// Str writes the index of s in the string table, adding s to it.
func (w *Encoder) Str(s string) {
	i, ok := w.Strs[s]
	if !ok {
		i = len(w.List)
		w.Strs[s] = i
		w.List = append(w.List, s)
	}
	w.Uint(i)
}

// Finish writes the string table and the body to out and appends the checksum.
func (w *Encoder) Finish(out *bytes.Buffer) {
	out.Write(binary.AppendUvarint(nil, uint64(len(w.List))))
	for _, s := range w.List {
		out.Write(binary.AppendUvarint(nil, uint64(len(s))))
		out.WriteString(s)
	}
	out.Write(w.Body.Bytes())
	out.Write(binary.LittleEndian.AppendUint32(nil, crc32.ChecksumIEEE(out.Bytes())))
}

// Decoder reads a compiled grammar. On invalid data it does not panic but records the first
// error.
type Decoder struct {
	Data []byte   // the compiled grammar
	Pos  int      // the read position in Data
	Strs []string // the string table
	Err  error    // the first error
}

// Fail records the error at the read position unless one is recorded.
func (r *Decoder) Fail(format string, args ...any) {
	if r.Err == nil {
		r.Err = fmt.Errorf("invalid compiled grammar at byte %d: %s", r.Pos, fmt.Sprintf(format, args...))
	}
}

// Byte reads a byte.
func (r *Decoder) Byte() byte {
	if r.Err != nil {
		return 0
	}
	if r.Pos >= len(r.Data) {
		r.Fail("unexpected end of data")
		return 0
	}
	b := r.Data[r.Pos]
	r.Pos++
	return b
}

// Uint reads a non-negative integer.
func (r *Decoder) Uint() int {
	if r.Err != nil {
		return 0
	}
	n, k := binary.Uvarint(r.Data[r.Pos:])
	if k <= 0 || n > 1<<31 {
		r.Fail("invalid integer")
		return 0
	}
	r.Pos += k
	return int(n)
}

// Count reads an element count. A count larger than the remaining data is invalid (this prevents
// huge allocations).
func (r *Decoder) Count() int {
	n := r.Uint()
	if n > len(r.Data)-r.Pos {
		r.Fail("count %d exceeds data", n)
		return 0
	}
	return n
}

// Int reads a signed integer.
func (r *Decoder) Int() int {
	if r.Err != nil {
		return 0
	}
	n, k := binary.Varint(r.Data[r.Pos:])
	if k <= 0 {
		r.Fail("invalid integer")
		return 0
	}
	r.Pos += k
	return int(n)
}

// Bool reads a boolean byte.
func (r *Decoder) Bool() bool {
	switch b := r.Byte(); b {
	case 0:
		return false
	case 1:
		return true
	default:
		r.Fail("invalid boolean %d", b)
		return false
	}
}

// Str reads a string by its index in the string table.
func (r *Decoder) Str() string {
	i := r.Uint()
	if r.Err != nil {
		return ""
	}
	if i >= len(r.Strs) {
		r.Fail("string index %d out of range", i)
		return ""
	}
	return r.Strs[i]
}

// Strings reads the string table and appends it to Strs.
func (r *Decoder) Strings() {
	n := r.Count()
	for i := 0; i < n && r.Err == nil; i++ {
		l := r.Count()
		if r.Err != nil {
			return
		}
		r.Strs = append(r.Strs, string(r.Data[r.Pos:r.Pos+l]))
		r.Pos += l
	}
}
