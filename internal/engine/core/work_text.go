package core

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// detachedText materializes input that cannot supply an original-source
// substring. Its local builder owns the output; input/scratch reuse cannot
// mutate a returned string. Allocation and Grow remain nonpreemptible.
func (in *Input) detachedText(start, end int) string {
	var out strings.Builder
	if in.Unit == Bytes {
		src := in.Bs[start:end]
		for off := 0; off < len(src); {
			n := min(4096, len(src)-off)
			in.Control.Charge(uint64(n), 1)
			if off == 0 {
				out.Grow(len(src))
			}
			out.Write(src[off : off+n])
			off += n
		}
		return out.String()
	}
	src := in.In[start:end]
	size := 0
	for off := 0; off < len(src); {
		n := min(128, len(src)-off)
		in.Control.Charge(uint64(n), uint64(n))
		for _, r := range src[off : off+n] {
			width := utf8.RuneLen(r)
			if width < 0 {
				width = utf8.RuneLen(utf8.RuneError)
			}
			size = CheckedTextSize(size, width)
		}
		off += n
	}
	out.Grow(size)
	for off := 0; off < len(src); {
		n := min(128, len(src)-off)
		in.Control.Charge(uint64(n), uint64(n))
		for _, r := range src[off : off+n] {
			out.WriteRune(r)
		}
		off += n
	}
	return out.String()
}

// CheckedTextSize prevents sizing overflow before allocating output storage.
func CheckedTextSize(size, width int) int {
	if size < 0 || width < 0 || size > int(^uint(0)>>1)-width {
		panic(Fatal{Err: fmt.Errorf("input text exceeds maximum string size")})
	}
	return size + width
}
