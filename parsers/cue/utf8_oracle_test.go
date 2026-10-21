package cue

import (
	"errors"
	"math/rand/v2"
	"strings"
	"testing"
	"unicode/utf8"
)

// oracleInvalidPos and oracleLocate are the straightforward rune loops that
// the scans in ParseFile and locate replaced: a literal reading of the
// position contract.
func oracleInvalidPos(input string, bytes bool) int {
	pos := 0
	for j, r := range input {
		if r == utf8.RuneError {
			if _, w := utf8.DecodeRuneInString(input[j:]); w == 1 {
				if bytes {
					return j
				}
				return pos
			}
		}
		pos++
	}
	return pos
}

func oracleLocate(input string, start int, bytes bool) (line, col int) {
	line, col = 1, 1
	i := 0
	for j, r := range input {
		if bytes && j >= start || !bytes && i >= start {
			break
		}
		if r == '\n' {
			line, col = line+1, 1
		} else {
			col++
		}
		i++
	}
	return line, col
}

func TestUTF8ScansMatchRuneLoops(t *testing.T) {
	pieces := []string{"a", "é", "😀", "\n", "\r\n", "\xff", "\xc3", "\xe2\x82", "\xf0\x9f\x98", "\x80", "\xed\xa0\x80", "�", " ", strings.Repeat("é", 300), strings.Repeat("a", 700), strings.Repeat("😀", 150)}
	rng := rand.New(rand.NewPCG(1, 2))
	for n := 0; n < 20000; n++ {
		var b strings.Builder
		for k := rng.IntN(12); k > 0; k-- {
			b.WriteString(pieces[rng.IntN(len(pieces))])
		}
		input := b.String()
		for _, bytes := range []bool{false, true} {
			unit := []PositionUnit{CodePoints}
			if bytes {
				unit = []PositionUnit{Bytes}
			}
			if !utf8.ValidString(input) {
				want := oracleInvalidPos(input, bytes)
				_, err := ParseFile(input, unit...)
				var se *SemanticError
				if !errors.As(err, &se) || se.Start != want {
					t.Fatalf("ParseFile(%q, bytes=%v) error = %v; want Start=%d", input, bytes, err, want)
				}
				if l, c := oracleLocate(input, want, bytes); se.Line != l || se.Col != c {
					t.Fatalf("ParseFile(%q, bytes=%v) at %d:%d; want %d:%d", input, bytes, se.Line, se.Col, l, c)
				}
			}
			// locate itself, at every position including past the end.
			step := 1 + len(input)/200 // every position of short input, a sample of long input
			for start := 0; start <= len(input)+1; start += step {
				e := &SemanticError{Span: Span{Start: start, End: start}}
				locate(input, e, unit)
				if l, c := oracleLocate(input, start, bytes); e.Line != l || e.Col != c {
					t.Fatalf("locate(%q, %d, bytes=%v) = %d:%d; want %d:%d", input, start, bytes, e.Line, e.Col, l, c)
				}
			}
		}
	}
}
