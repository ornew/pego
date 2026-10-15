package engine

import "unicode/utf8"

// prefixByteOffset translates a successful batch cursor back to the original
// source bytes. end is in the input's Unit and must be a successful cursor in
// source. This is separate from text(): malformed UTF-8 is normalized in node
// text, but consumed bytes must still refer to the original source snapshot.
func (in *input) prefixByteOffset(source string, end int) int {
	if end == 0 || in.unit == Bytes {
		return end
	}
	if len(in.offs) > end {
		return int(in.offs[end])
	}
	// Recognition need not build the lazy offset table, and malformed input
	// cannot use that table. Scan only the consumed prefix, without retaining
	// another index. Each malformed byte is one decoded replacement scalar.
	off := 0
	for i := 0; i < end; i++ {
		in.inputWork(uint64(min(utf8.UTFMax, len(source)-off)))
		_, size := utf8.DecodeRuneInString(source[off:])
		off += size
	}
	return off
}
