package typescript_test

import (
	"flag"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	tscMutations = flag.Int("tsc.mutations", 3000, "the number of mutated sources TestTSCMutations compares")
	tscSeed      = flag.Uint64("tsc.seed", 1, "the seed of TestTSCMutations")
)

// TestTypeScriptLib compares the trees of the declaration files of the TypeScript package (lib/*.d.ts), large
// real-world inputs, with the compiler's.
func TestTypeScriptLib(t *testing.T) {
	p := startTSC(t)
	files, _ := filepath.Glob(filepath.Join(os.Getenv("PEGO_TYPESCRIPT"), "lib", "*.d.ts"))
	if len(files) == 0 {
		t.Skip("no lib/*.d.ts in PEGO_TYPESCRIPT")
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		d, _, err := compareWithTSC(p, filepath.Base(f), string(data))
		if err != nil {
			t.Fatal(err)
		}
		if d != "" {
			t.Errorf("%s: %s", filepath.Base(f), d)
		}
	}
	t.Logf("%d files", len(files))
}

// tokenRegex splits a source into rough tokens for TestTSCMutations: comments, strings, templates,
// numbers, words, line breaks, and punctuation of up to four characters.
var tokenRegex = regexp.MustCompile("(?s)//[^\n]*|/\\*.*?\\*/|\"(?:[^\"\\\\\n]|\\\\.)*\"|'(?:[^'\\\\\n]|\\\\.)*'|`(?:[^`\\\\]|\\\\.)*`" +
	"|[0-9][0-9a-zA-Z_.]*|[A-Za-z_$#][A-Za-z0-9_$]*|\n|>>>=|\\.\\.\\.|[=!]==|\\*\\*=|<<=|>>=|&&=|\\|\\|=|\\?\\?=|=>|\\?\\.|[-+*/%&|^<>=!?]=?|[-+&|?]{2}|[^ \t\r]")

// mutationTokens are inserted or substituted by TestTSCMutations: the tokens where the grammar decides.
var mutationTokens = []string{
	";", ",", "(", ")", "{", "}", "[", "]", "<", ">", "=>", "?", ":", ".", "?.", "!", "=", "...", "@", "*", "/",
	"\n", "+", "++", "-", "&", "|", "`", "${", "#x", "x", "1", "\"s\"", "as", "satisfies", "in", "of", "of", "let",
	"const", "var", "async", "await", "yield", "type", "interface", "namespace", "declare", "abstract", "export",
	"import", "default", "function", "class", "new", "this", "super", "extends", "implements", "keyof", "infer",
	"readonly", "unique", "static", "get", "set", "accessor", "public", "private", "using", "enum", "return",
	"if", "else", "for", "while", "do", "switch", "case", "throw", "try", "catch", "finally", "is", "asserts",
	"typeof", "void", "delete", "null", "true", "<T>", "<T,>", "</", "/>", "<div>", "</div>",
}

// TestTSCMutations compares acceptance (and the trees of accepted sources) with the compiler on sources made
// by mutating test cases of the TypeScript repository: a token deleted, duplicated, replaced or inserted,
// or two tokens swapped. The mutations are deterministic: -tsc.seed sets the seed and -tsc.mutations their
// number.
func TestTSCMutations(t *testing.T) {
	cases := os.Getenv("PEGO_TYPESCRIPT_TESTS")
	if cases == "" {
		t.Skip("PEGO_TYPESCRIPT_TESTS is not set")
	}
	p := startTSC(t)
	var units []testUnit
	for _, dir := range []string{"conformance", "compiler"} {
		filepath.WalkDir(filepath.Join(cases, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !isTypeScriptFile(path) {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			for _, u := range splitTestCase(path, decodeTestFile(data)) {
				if isTypeScriptFile(u.name) && len(u.content) < 4000 {
					units = append(units, u)
				}
			}
			return nil
		})
	}
	sort.Slice(units, func(i, j int) bool { return units[i].name+units[i].content < units[j].name+units[j].content })
	if len(units) == 0 {
		t.Fatal("no test cases")
	}
	r := rand.New(rand.NewPCG(*tscSeed, 11))
	fails := 0
	for i := range *tscMutations {
		u := units[r.IntN(len(units))]
		toks := tokenRegex.FindAllStringIndex(u.content, -1)
		if len(toks) == 0 {
			continue
		}
		src := mutate(r, u.content, toks)
		d, _, err := compareWithTSC(p, u.name, src)
		if err != nil {
			t.Fatal(err)
		}
		if d != "" {
			fails++
			if fails <= 50 {
				t.Errorf("mutation %d of %s:\n%s\n--- %s", i, u.name, src, d)
			}
		}
	}
	if fails > 0 {
		t.Errorf("%d of %d mutations differ", fails, *tscMutations)
	}
}

// mutate applies one random token mutation to src, whose tokens are at toks.
func mutate(r *rand.Rand, src string, toks [][]int) string {
	k := toks[r.IntN(len(toks))]
	tok := src[k[0]:k[1]]
	other := mutationTokens[r.IntN(len(mutationTokens))]
	switch r.IntN(5) {
	case 0: // delete
		return src[:k[0]] + src[k[1]:]
	case 1: // duplicate
		return src[:k[1]] + " " + tok + src[k[1]:]
	case 2: // replace
		return src[:k[0]] + other + src[k[1]:]
	case 3: // insert
		return src[:k[0]] + other + " " + src[k[0]:]
	default: // swap with the next token
		var next []int
		for _, n := range toks {
			if n[0] >= k[1] {
				next = n
				break
			}
		}
		if next == nil {
			return src[:k[0]] + src[k[1]:]
		}
		return src[:k[0]] + src[next[0]:next[1]] + src[k[1]:next[0]] + tok + src[next[1]:]
	}
}

var _ = strings.Contains
