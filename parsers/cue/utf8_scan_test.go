package cue

import (
	"errors"
	"strings"
	"testing"
)

// Positions of invalid UTF-8, at the start, the middle and the end of the
// input, in long lines, after many lines, and next to multi-byte characters.
// The expected values are computed by hand, not by a second implementation.
func TestInvalidUTF8PositionsInLongInput(t *testing.T) {
	long := strings.Repeat("a", 1000)
	wide := strings.Repeat("é😀", 300) // 300 * (2+4) bytes, 600 code points
	lines := strings.Repeat("// é\n", 50)
	for _, tc := range []struct {
		name, input       string
		codepoints, bytes int
		line, col         int
	}{
		{"start", "\xffx: 1\n", 0, 0, 1, 1},
		{"middle of long ASCII", "// " + long + "\xff" + long + "\n", 1003, 1003, 1, 1004},
		{"end of long ASCII", "// " + long + "\xff", 1003, 1003, 1, 1004},
		{"middle of wide text", "// " + wide + "\xff" + wide + "\n", 603, 1803, 1, 604},
		{"end of wide text", "// " + wide + "\xff", 603, 1803, 1, 604},
		{"after many lines", lines + "// é\xff", 50*5 + 4, 50*6 + 5, 51, 5},
		{"last byte of input after newline", lines + "\xff", 250, 300, 51, 1},
		{"truncated 2-byte at end", "// a\xc3", 4, 4, 1, 5},
		{"truncated 3-byte at end", "// é\xe2\x82", 4, 5, 1, 5},
		{"truncated 4-byte at end", "// é\xf0\x9f\x98", 4, 5, 1, 5},
		{"truncated 3-byte before ASCII", "// é\xe2\x82 x", 4, 5, 1, 5},
		{"truncated 4-byte before multi-byte", "// 😀\xf0\x9f\x98é", 4, 7, 1, 5},
		{"directly after 4-byte", "// 😀\xff", 4, 7, 1, 5},
		{"directly before 4-byte", "// \xff😀", 3, 3, 1, 4},
		{"between two 2-byte", "// é\xffé", 4, 5, 1, 5},
		{"only continuation byte", "\x80", 0, 0, 1, 1},
		{"after only replacement runes", "�����\xff", 5, 15, 1, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range []struct {
				name string
				unit []PositionUnit
				pos  int
			}{
				{"default", nil, tc.codepoints},
				{"CodePoints", []PositionUnit{CodePoints}, tc.codepoints},
				{"Bytes", []PositionUnit{Bytes}, tc.bytes},
			} {
				_, err := ParseFile(tc.input, mode.unit...)
				var se *SemanticError
				if !errors.As(err, &se) {
					t.Fatalf("%s: error %v; want SemanticError", mode.name, err)
				}
				if se.Start != mode.pos || se.End != mode.pos || se.Line != tc.line || se.Col != tc.col {
					t.Errorf("%s: error = %+v; want span [%d,%d), %d:%d", mode.name, *se, mode.pos, mode.pos, tc.line, tc.col)
				}
			}
		})
	}
}

// A semantic error found by Check gets its line and column from locate, for a
// position given in code points or in bytes.
func TestSemanticErrorLocationInLongInput(t *testing.T) {
	long := strings.Repeat("a", 1000)
	dup := "{X=a: 1, X=b: 2}\n"
	for _, tc := range []struct {
		name, input       string
		codepoints, bytes int // Start of the error
		line, col         int
	}{
		{"first line", dup, 9, 9, 1, 10},
		{"long first line", "{\"é" + long + "\": 1, X=a: 1, X=b: 2}\n", 1017, 1018, 1, 1018},
		{"after multi-byte lines", "// é😀\n// é😀\n" + dup, 2*6 + 9, 2*10 + 9, 3, 10},
		{"after wide text on the line", "{\"é😀é😀\": 1, X=a: 1, X=b: 2}\n", 20, 28, 1, 21},
		{"after blank lines", "\n\n\n" + dup, 12, 12, 4, 10},
		{"after CRLF", "// é\r\n" + dup, 15, 16, 2, 10},
	} {
		for _, mode := range []struct {
			name string
			unit PositionUnit
			pos  int
		}{{"CodePoints", CodePoints, tc.codepoints}, {"Bytes", Bytes, tc.bytes}} {
			t.Run(tc.name+"/"+mode.name, func(t *testing.T) {
				_, err := ParseFile(tc.input, mode.unit)
				var se *SemanticError
				if !errors.As(err, &se) {
					t.Fatalf("error %v; want SemanticError", err)
				}
				if se.Start != mode.pos || se.Line != tc.line || se.Col != tc.col {
					t.Errorf("error = %+v; want Start=%d, %d:%d", *se, mode.pos, tc.line, tc.col)
				}
			})
		}
	}
}

// BenchmarkLocate measures the line and column of a semantic error (a
// redeclared alias) at the end of a long input, in code points and in bytes.
func BenchmarkLocate(b *testing.B) {
	for _, tc := range []struct{ name, input string }{
		{"ASCII", strings.Repeat("// "+strings.Repeat("a", 110)+"\n", 512) + "{X=a: 1, X=b: 2}\n"},
		{"Unicode", strings.Repeat("// "+strings.Repeat("é", 40)+strings.Repeat("😀", 20)+"\n", 512) + "{X=a: 1, X=b: 2}\n"},
		{"one line", "{\"" + strings.Repeat("é😀", 20000) + "\": 1, X=a: 1, X=b: 2}\n"},
	} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			name := "CodePoints"
			if unit == Bytes {
				name = "Bytes"
			}
			b.Run(tc.name+"/"+name, func(b *testing.B) {
				b.SetBytes(int64(len(tc.input)))
				b.ReportAllocs()
				for b.Loop() {
					if _, err := ParseFile(tc.input, unit); err == nil {
						b.Fatal("duplicate alias accepted")
					}
				}
			})
		}
	}
}
