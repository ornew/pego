package cue

import (
	"bufio"
	"compress/gzip"
	"math/big"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestLiterals compares the values that Unquote, Int.Value and Float.Rat decode with those of the reference
// implementation (cuelang.org/go/cue/literal), recorded in testdata/literals.txt.gz by internal/refgen: for the
// string, integer and float literals of the corpus and of generated inputs, whether the decoding fails, and the
// value (a number as a decimal text, compared exactly).
func TestLiterals(t *testing.T) {
	f, err := os.Open("testdata/literals.txt.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(gz)
	sc.Buffer(nil, 1<<20)
	counts := map[string]int{}
	bad := 0
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			t.Fatalf("bad line %q", line)
		}
		kind := fields[0]
		lit, err := strconv.Unquote(fields[1])
		if err != nil {
			t.Fatalf("bad line %q: %v", line, err)
		}
		wantOK := fields[2] == "ok"
		var want string
		if wantOK {
			if want, err = strconv.Unquote(fields[3]); err != nil {
				t.Fatalf("bad line %q: %v", line, err)
			}
		}
		var got string
		var gotErr error
		file, err := ParseAST("x: " + lit)
		if err != nil {
			t.Errorf("%s %q: %v", kind, lit, err)
			bad++
			continue
		}
		v := file.Decls[0].(*Field).Value
		switch kind {
		case "S":
			s, ok := v.(*String)
			if !ok {
				t.Errorf("%s %q: parsed as %T", kind, lit, v)
				bad++
				continue
			}
			got, gotErr = s.Unquote()
		case "I":
			n, ok := v.(*Int)
			if !ok {
				t.Errorf("%s %q: parsed as %T", kind, lit, v)
				bad++
				continue
			}
			var b *big.Int
			if b, gotErr = n.Value(); gotErr == nil {
				got = b.String()
			}
		case "F":
			n, ok := v.(*Float)
			if !ok {
				t.Errorf("%s %q: parsed as %T", kind, lit, v)
				bad++
				continue
			}
			var r *big.Rat
			if r, gotErr = n.Rat(); gotErr == nil {
				// Compare as numbers: the reference keeps the digits it was given (1.00).
				w, ok := new(big.Rat).SetString(want)
				if !wantOK || !ok {
					got = r.String()
				} else if r.Cmp(w) == 0 {
					got = want
				} else {
					got = r.String()
				}
			}
		}
		counts[kind]++
		if kind == "I" && wantOK && gotErr == nil {
			// An integer is compared as a number too.
			if w, ok := new(big.Int).SetString(want, 10); ok && w.String() == got {
				continue
			}
		}
		if (gotErr == nil) != wantOK || gotErr == nil && got != want {
			bad++
			if bad <= 30 {
				t.Errorf("%s %q: got %q, %v; the reference has %q (ok %v)", kind, lit, got, gotErr, want, wantOK)
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("literals compared: %d strings, %d integers, %d floats; %d differ", counts["S"], counts["I"], counts["F"], bad)
}
