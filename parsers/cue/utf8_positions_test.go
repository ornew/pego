package cue

import (
	"errors"
	"strings"
	"testing"
)

func TestInvalidUTF8Positions(t *testing.T) {
	for _, tc := range []struct {
		name, input       string
		codepoints, bytes int
		line, col         int
	}{
		{"first byte", "\xff", 0, 0, 1, 1},
		{"ASCII prefix", "x: \"a\xffb\"\n", 5, 5, 1, 6},
		{"two-byte prefix", "x: \"é\xff\"", 5, 6, 1, 6},
		{"four-byte prefix", "x: \"😀\xff\"", 5, 8, 1, 6},
		{"replacement rune", "x: \"�\xff\"", 5, 7, 1, 6},
		{"later line", "é: 1\nx: \"é😀\xff\"", 11, 16, 2, 7},
		{"after newline", "é: 1\n\xff", 5, 6, 2, 1},
		{"CRLF", "é: 1\r\nx: \"é\xff\"", 11, 13, 2, 6},
		{"truncated lead", "x: \"é\xc3", 5, 6, 1, 6},
		{"bad continuation", "x: \"é\xe2(\xa1\"", 5, 6, 1, 6},
		{"continuation alone", "x: \"é\x80\"", 5, 6, 1, 6},
		{"overlong encoding", "x: \"é\xc0\xaf\"", 5, 6, 1, 6},
		{"surrogate encoding", "x: \"é\xed\xa0\x80\"", 5, 6, 1, 6},
		{"out of range", "x: \"é\xf4\x90\x80\x80\"", 5, 6, 1, 6},
		{"first of multiple", "x: \"é\xff\xfe\"", 5, 6, 1, 6},
		{"comment", "// é😀\xff\n", 5, 9, 1, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range []struct {
				name string
				unit []Unit
				pos  int
			}{
				{"default", nil, tc.codepoints},
				{"CodePoints", []Unit{CodePoints}, tc.codepoints},
				{"Bytes", []Unit{Bytes}, tc.bytes},
			} {
				t.Run(mode.name, func(t *testing.T) {
					f, err := ParseFile(tc.input, mode.unit...)
					var se *SemanticError
					if f != nil || !errors.As(err, &se) {
						t.Fatalf("ParseFile = %v, %v; want nil and SemanticError", f, err)
					}
					if se.Start != mode.pos || se.End != mode.pos || se.Line != tc.line || se.Col != tc.col || se.Msg != "illegal UTF-8 encoding" {
						t.Errorf("error = %+v; want span [%d,%d), %d:%d, illegal UTF-8 encoding", *se, mode.pos, mode.pos, tc.line, tc.col)
					}
				})
			}
		})
	}
}

func TestSemanticErrorColumnsWithUnicode(t *testing.T) {
	for _, tc := range []struct {
		unit Unit
		pos  int
	}{{CodePoints, 15}, {Bytes, 16}} {
		_, err := ParseFile("{é: 1, X=a: 2, X=b: 3}\n", tc.unit)
		var se *SemanticError
		if !errors.As(err, &se) {
			t.Fatalf("error %v; want SemanticError", err)
		}
		if se.Start != tc.pos || se.Line != 1 || se.Col != 16 || se.Msg != `alias "X" redeclared in same scope` {
			t.Errorf("%v: error = %+v; want Start=%d, 1:16, duplicate alias", tc.unit, *se, tc.pos)
		}
	}
	for _, input := range []string{"x: \"�\"", "// �\nx: 1"} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			if _, err := ParseFile(input, unit); err != nil {
				t.Errorf("valid replacement rune in %q: %v", input, err)
			}
		}
	}
}

// Both revisions reject these inputs; only the error positions change. Include
// the public API's validation, first-error search and line/column calculation.
func BenchmarkInvalidUTF8Position(b *testing.B) {
	for _, tc := range []struct{ name, input string }{
		{"first", "\xff"},
		{"ASCII tail", "// " + strings.Repeat("a", 64<<10) + "\xff"},
		{"Unicode tail", "// " + strings.Repeat("é😀", (64<<10)/6) + "\xff"},
	} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			name := "CodePoints"
			if unit == Bytes {
				name = "Bytes"
			}
			b.Run(tc.name+"/"+name, func(b *testing.B) {
				b.SetBytes(int64(len(tc.input)))
				b.ReportAllocs()
				for b.Loop() {
					if _, err := ParseFile(tc.input, unit); err == nil {
						b.Fatal("invalid UTF-8 accepted")
					}
				}
			})
		}
	}
}
