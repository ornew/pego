package cue

import (
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"
)

// FuzzParse checks that the three parsers agree (ParseAST, Parse and Recognize accept the same inputs), that no
// input makes them panic, and that the spans of an accepted file are inside the input and nested in order.
//
//	go test -fuzz FuzzParse -fuzztime 1m
func FuzzParse(f *testing.F) {
	files, _ := filepath.Glob("testdata/*.txt")
	for _, name := range files {
		if b, err := os.ReadFile(name); err == nil {
			f.Add(string(b))
		}
	}
	for _, s := range []string{
		"", "a: 1", "a: b: c: 1\n", "x: [for y in z {y}]", "x: #\"\"\"\n  a\n  \"\"\"#", "x: \"\\(a + \"\\(b)\")\"",
		"@experiment(try)\nx: try {a: 1}", "a~X: 1", "a: b?: c, d!: e", "if x {y: 1}", "let a = 1", "a: 1 @b(c(d), \"e\")",
		"import \"strings\"\nx: strings.ToUpper(\"a\")", "x: 1 | *2 & >=3 & <5", "x: f(1, 2)[0].a.b?", "...", "_|_",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		if !utf8.ValidString(in) {
			return
		}
		file, aerr := ParseAST(in)
		rerr := Recognize(in)
		_, perr := Parse(in)
		if (aerr == nil) != (rerr == nil) || (aerr == nil) != (perr == nil) {
			t.Fatalf("%q: ParseAST: %v; Parse: %v; Recognize: %v", in, aerr, perr, rerr)
		}
		_ = Comments(in)
		if aerr != nil {
			return
		}
		_ = file.Check()
		n := utf8.RuneCountInString(in)
		var walk func(x any, lo, hi int)
		walk = func(x any, lo, hi int) {
			sp := SpanOf(x)
			if sp.Start < lo || sp.End > hi || sp.Start > sp.End {
				t.Fatalf("%q: span %v of %T is not inside %d..%d", in, sp, x, lo, hi)
			}
			for _, ch := range children(x) {
				walk(ch, sp.Start, sp.End)
			}
		}
		walk(file, 0, n)
	})
}
