package core

import "unicode/utf8"

// PrefixByteOffset translates a successful batch cursor back to the original
// source bytes. end is in the input's Unit and must be a successful cursor in
// source. This is separate from text(): malformed UTF-8 is normalized in node
// text, but consumed bytes must still refer to the original source snapshot.
func (in *Input) PrefixByteOffset(source string, end int) int {
	if end == 0 || in.Unit == Bytes {
		return end
	}
	if len(in.Offs) > end {
		return int(in.Offs[end])
	}
	// Recognition need not build the lazy offset table, and malformed input
	// cannot use that table. Scan only the consumed prefix, without retaining
	// another index. Each malformed byte is one decoded replacement scalar.
	off := 0
	for i := 0; i < end; i++ {
		in.spendRead(uint64(min(utf8.UTFMax, len(source)-off)))
		_, size := utf8.DecodeRuneInString(source[off:])
		off += size
	}
	return off
}
