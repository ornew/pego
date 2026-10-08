package json_test

import (
	"bytes"
	stdjson "encoding/json"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/json"
)

// TestJSONTestSuite runs the parsing tests of JSONTestSuite: y_ files must be accepted and decode to
// what encoding/json decodes, n_ files must be rejected, and i_ files may go either way, but Recognize
// and ParseAST must agree.
func TestJSONTestSuite(t *testing.T) {
	files, err := filepath.Glob("testdata/JSONTestSuite/test_parsing/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no test files: %v", err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(f)
		_, perr := json.ParseAST(string(data))
		rerr := json.Recognize(string(data))
		if (perr == nil) != (rerr == nil) {
			t.Errorf("%s: ParseAST: %v, Recognize: %v", name, perr, rerr)
		}
		switch name[0] {
		case 'y':
			if perr != nil {
				t.Errorf("%s: rejected: %v", name, perr)
				continue
			}
			checkDecode(t, name, string(data))
		case 'n':
			if perr == nil {
				t.Errorf("%s: accepted", name)
			}
		case 'i':
			if perr == nil {
				checkDecode(t, name, string(data))
			}
		}
	}
}

// checkDecode compares Decode with encoding/json on an accepted input.
func checkDecode(t *testing.T, name, src string) {
	t.Helper()
	var want any
	werr := stdjson.Unmarshal([]byte(src), &want)
	got, gerr := json.Decode(src)
	if (werr == nil) != (gerr == nil) {
		t.Errorf("%s: Decode: %v, encoding/json: %v", name, gerr, werr)
		return
	}
	if werr == nil && !reflect.DeepEqual(got, want) {
		t.Errorf("%s: Decode: %#v, encoding/json: %#v", name, got, want)
	}
}

func TestDecode(t *testing.T) {
	for _, src := range []string{
		`null`, `true`, `false`, `0`, `-0`, `-12.5e+3`, `1E-2`, `"a\"b\\c\/dé\n\b\f\r\t"`,
		`[]`, `{}`, `[1, [2, [3]], {"k": []}]`,
		`{"name": "pego", "tags": ["peg", "pratt"], "ok": true, "n": null, "v": 1.5}`,
		" \n\t{ \"a\" : { \"b\" : [ 1 , 2 ] } } \n",
		`"日本語"`, `"日本"`, `"😀"`, `"\ud83d"`, `"\ude00\ud83d"`, `"\ud83dx"`, `"\ud83dA"`,
		`{"a": 1, "a": 2}`, "\"\xff\xfe\"", "\"a\xc3\"",
		`1e400`, `-1e400`, `123456789012345678901234567890`,
	} {
		checkDecode(t, src, src)
	}
}

func TestReject(t *testing.T) {
	for _, src := range []string{
		``, ` `, `{`, `[1,]`, `{"a" 1}`, `{"a":1,}`, `01`, `1.`, `.5`, `+1`, `"\x"`, "\"a\nb\"", "\"\x00\"",
		`tru`, `[1] [2]`, `{a: 1}`, `'a'`, `NaN`, `[1 2]`, `"\u12"`, "\ufeff{}", `// c`, "\v1",
	} {
		var v any
		if stdjson.Unmarshal([]byte(src), &v) == nil {
			t.Fatalf("encoding/json accepted %q", src)
		}
		if json.Valid(src) {
			t.Errorf("%q: accepted", src)
		}
		if _, err := json.Decode(src); err == nil {
			t.Errorf("%q: Decode: expected an error", src)
		}
	}
}

// TestRandom compares Decode with encoding/json on documents generated from random values.
func TestRandom(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for i := range 2000 {
		data, err := stdjson.Marshal(randomValue(r, 4))
		if err != nil {
			t.Fatal(err)
		}
		src := string(data)
		if i%2 == 1 {
			src = indent(data)
		}
		checkDecode(t, src, src)
	}
}

func randomValue(r *rand.Rand, depth int) any {
	k := r.IntN(7)
	if depth == 0 {
		k = r.IntN(4)
	}
	switch k {
	case 0:
		return nil
	case 1:
		return r.IntN(2) == 0
	case 2:
		switch r.IntN(3) {
		case 0:
			return r.NormFloat64() * 1e6
		case 1:
			return float64(r.Int64N(1 << 53))
		}
		return r.Float64() * 1e-300
	case 3:
		return randomString(r)
	case 4, 5:
		a := make([]any, r.IntN(5))
		for i := range a {
			a[i] = randomValue(r, depth-1)
		}
		return a
	}
	m := map[string]any{}
	for range r.IntN(5) {
		m[randomString(r)] = randomValue(r, depth-1)
	}
	return m
}

func randomString(r *rand.Rand) string {
	const chars = "aZ09 \"\\/\b\f\n\r\t\x00\x1f\x7fé日😀 <>&"
	rs := []rune(chars)
	var b strings.Builder
	for range r.IntN(8) {
		b.WriteRune(rs[r.IntN(len(rs))])
	}
	return b.String()
}

func indent(data []byte) string {
	var buf bytes.Buffer
	if err := stdjson.Indent(&buf, data, "", "\t"); err != nil {
		panic(err)
	}
	return buf.String()
}

// FuzzDecode compares the parser with encoding/json on arbitrary input.
func FuzzDecode(f *testing.F) {
	for _, s := range []string{`{"a":[1,true,null,"x"]}`, `-0.5e10`, `"😀"`, `[1,]`, `{"a" 1}`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		var want any
		werr := stdjson.Unmarshal([]byte(src), &want)
		valid := json.Valid(src)
		if valid != stdjson.Valid([]byte(src)) {
			t.Fatalf("%q: Valid %v, encoding/json %v", src, valid, !valid)
		}
		got, gerr := json.Decode(src)
		if valid && (werr == nil) != (gerr == nil) {
			t.Fatalf("%q: Decode: %v, encoding/json: %v", src, gerr, werr)
		}
		if !valid && gerr == nil {
			t.Fatalf("%q: Decode accepted invalid input", src)
		}
		if werr == nil && !reflect.DeepEqual(got, want) {
			t.Fatalf("%q: Decode: %#v, encoding/json: %#v", src, got, want)
		}
	})
}

// TestGolden checks the generated parser against the golden files of testdata, which the tests of
// github.com/ornew/pego/parsers check every backend of the engine against.
func TestGolden(t *testing.T) {
	inputs, _ := filepath.Glob("testdata/*.txt")
	if len(inputs) == 0 {
		t.Fatal("no inputs")
	}
	for _, in := range inputs {
		data, err := os.ReadFile(in)
		if err != nil {
			t.Fatal(err)
		}
		var got string
		n, err := json.Parse(string(data))
		if n != nil {
			got = n.String()
		}
		if err != nil {
			if got != "" {
				got += "\n"
			}
			got += "error: " + err.Error()
		}
		want, err := os.ReadFile(strings.TrimSuffix(in, ".txt") + ".golden")
		if err != nil {
			t.Fatal(err)
		}
		if got+"\n" != string(want) {
			t.Errorf("%s\n got  %s\n want %s", in, got, want)
		}
	}
}

// TestDeepNesting checks that nesting up to the depth limit parses and that deeper nesting is an error,
// not a stack overflow.
func TestDeepNesting(t *testing.T) {
	for _, tc := range []struct {
		depth int
		ok    bool
	}{{30000, true}, {40000, false}, {1000000, false}} {
		src := strings.Repeat("[", tc.depth) + strings.Repeat("]", tc.depth)
		_, err := json.ParseAST(src)
		if (err == nil) != tc.ok {
			t.Errorf("depth %d: ParseAST: %v", tc.depth, err)
		}
		if err := json.Recognize(src); (err == nil) != tc.ok {
			t.Errorf("depth %d: Recognize: %v", tc.depth, err)
		}
		if _, err := json.Decode(src); (err == nil) != tc.ok {
			t.Errorf("depth %d: Decode: %v", tc.depth, err)
		}
	}
}
