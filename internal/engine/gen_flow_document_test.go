package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine/flow"
)

// The Node tables of a Documents build are printed from Flow IR (Options.Document). A Document parse records how
// far it examined the input in the primitives that read it, and reuses a result after an edit only if the edit
// lies outside that range; a read in place that bypasses the primitive makes a Document return a stale result.
// The tests build each parser twice, with the rules printed from Flow IR and with dgen (directNodeRules,
// directRecognizer), and run the same random edit sequence on a Document of each, comparing after every edit the
// result, the error, the evaluation and reuse counts of the parse (which depend on the ranges recorded) and
// the result of a parse from scratch of the same text.

// documentProbeMain is the consumer of the generated packages with the prefix PREFIX. For each input it creates a
// Document, applies the edits of a generator seeded by the input, parses after each edit twice (the second parse
// reuses everything) and prints the number of steps, the hash of everything observed, and the number of results
// that differ from the parse from scratch.
const documentProbeMain = `package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	pegort "github.com/ornew/pego/runtime"
IMPORTS)

type doc struct {
	parse func() (*pegort.Node, error)
	edit  func(int, int, string) error
	text  func() string
	stats func() pegort.ParseStats
}

type entry struct {
	newDoc func(string) (*doc, error)
	parse  func(string) (*pegort.Node, error)
}

var entries = []entry{
ENTRIES}

type input struct {
	Text  string
	Edits int
	Alpha string
}

func main() {
	var inputs [][]input
	data, _ := os.ReadFile(os.Args[1])
	json.Unmarshal(data, &inputs)
	for i := range inputs {
		for j, in := range inputs[i] {
			fmt.Println(run(fmt.Sprintf("%d/%d", i, j), entries[i], in))
		}
	}
}

func run(id string, e entry, in input) string {
	h := sha256.New()
	steps, bad, firstBad := 0, 0, -1
	alpha := []rune(in.Alpha)
	state := uint64(len(in.Text))*2654435761 + 12345
	next := func(n int) int {
		state = state*6364136223846793005 + 1442695040888963407
		return int((state >> 33) % uint64(n))
	}
	d, err := e.newDoc(in.Text)
	if err != nil {
		return fmt.Sprintf("%s steps=0 digest=%q bad=0 first=-1", id, err.Error())
	}
	observe := func() {
		for k := 0; k < 2; k++ {
			n, err := d.parse()
			st := d.stats()
			fresh, ferr := e.parse(d.text())
			a, _ := json.Marshal(n)
			b, _ := json.Marshal(fresh)
			same := string(a) == string(b) && fmt.Sprint(err) == fmt.Sprint(ferr)
			if !same {
				bad++
				if firstBad < 0 {
					firstBad = steps
				}
			}
			fmt.Fprintf(h, "%s|%v|%d|%d|%v;", a, err, st.Evaluated, st.Reused, same)
		}
		steps++
	}
	observe()
	for k := 0; k < in.Edits; k++ {
		n := utf8.RuneCountInString(d.text())
		var s, t int
		var repl string
		pick := func(max int) string {
			var b strings.Builder
			for m := next(max) + 1; m > 0; m-- {
				b.WriteRune(alpha[next(len(alpha))])
			}
			return b.String()
		}
		switch next(3) {
		case 0:
			s = next(n + 1)
			t, repl = s, pick(3)
		case 1:
			if n == 0 {
				continue
			}
			s = next(n)
			t = s + next(minInt(3, n-s)) + 1
		default:
			if n == 0 {
				continue
			}
			s = next(n)
			t = s + next(minInt(3, n-s)) + 1
			repl = pick(2)
		}
		err := d.edit(s, t, repl)
		fmt.Fprintf(h, "edit %d %d %q %v;", s, t, repl, err)
		if err != nil {
			continue
		}
		observe()
	}
	return fmt.Sprintf("%s steps=%d digest=%x bad=%d first=%d", id, steps, h.Sum(nil)[:8], bad, firstBad)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
`

var documentLine = regexp.MustCompile(`^(\S+) steps=(\d+) digest=(\S+) bad=(\d+) first=(-?\d+)$`)

type documentInput struct {
	Text  string
	Edits int
	Alpha string
}

// documentCase is a grammar with the inputs of its Documents and the number of edits applied to each.
type documentCase struct {
	genCase
	edits int
}

func documentInputs(c documentCase) []documentInput {
	seen := map[rune]bool{}
	var alpha []rune
	add := func(r rune) {
		if !seen[r] {
			seen[r] = true
			alpha = append(alpha, r)
		}
	}
	for _, in := range c.inputs {
		for _, r := range in {
			if len(alpha) < 48 {
				add(r)
			}
		}
	}
	for _, r := range " \n;,()+-x1" {
		add(r)
	}
	var out []documentInput
	for _, in := range append([]string{""}, c.inputs...) {
		out = append(out, documentInput{in, c.edits, string(alpha)})
	}
	return out
}

// runDocumentProbes builds the cases as the packages f0..fN (Flow IR, mutated by mutate if not nil) and d0..dN
// (dgen) with Documents and returns the report line of the consumer for each input of each build; the lines
// of a build are nil if its consumer fails and mutate is set.
func runDocumentProbes(t *testing.T, cases []documentCase, start string, mutate func([]byte) []byte) (flowOut, directOut []string) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", testGoMod("docprobe"))
	var inputs [][]documentInput
	for i, c := range cases {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, form := range []struct {
			prefix string
			opts   GenOptions
		}{{"f", GenOptions{}}, {"d", GenOptions{directNodeRules: true, directRecognizer: true}}} {
			o := form.opts
			o.Package, o.Start, o.Recognize, o.Document = fmt.Sprintf("%s%d", form.prefix, i), start, true, true
			code, err := generateCode(g, o)
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			if form.prefix == "f" && mutate != nil {
				code = mutate(code)
			}
			write(fmt.Sprintf("%s%d/parser.go", form.prefix, i), string(code))
		}
		inputs = append(inputs, documentInputs(c))
	}
	run := func(prefix string) []string {
		var imports, entries strings.Builder
		for i := range cases {
			fmt.Fprintf(&imports, "\t%s%d \"docprobe/%s%d\"\n", prefix, i, prefix, i)
			fmt.Fprintf(&entries, `	{newDoc: func(s string) (*doc, error) {
		d, err := %[1]s%[2]d.NewDocument(s)
		if err != nil {
			return nil, err
		}
		return &doc{func() (*pegort.Node, error) { return d.Parse() }, d.Edit, d.Text, d.Stats}, nil
	}, parse: func(s string) (*pegort.Node, error) { return %[1]s%[2]d.Parse(s) }},
`, prefix, i)
		}
		write("main.go", strings.NewReplacer("IMPORTS", imports.String(), "ENTRIES", entries.String()).Replace(documentProbeMain))
		data, _ := json.Marshal(inputs)
		write("inputs.json", string(data))
		cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".", "inputs.json")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			if mutate != nil && prefix == "f" {
				return nil
			}
			t.Fatalf("go run (%s): %v\n%s", prefix, err, stderr.String())
		}
		var lines []string
		for _, l := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
			if !documentLine.MatchString(l) {
				t.Fatalf("go run (%s): malformed probe line %q", prefix, l)
			}
			lines = append(lines, l)
		}
		return lines
	}
	return run("f"), run("d")
}

// documentDiffs returns the ids of the reports that differ, and the number of results of the direct build that
// differ from a parse from scratch, which must be none.
func documentDiffs(t *testing.T, f, d []string) (differing []string) {
	t.Helper()
	if len(f) != len(d) || len(f) == 0 {
		t.Fatalf("%d reports from the Flow build and %d from the direct build", len(f), len(d))
	}
	steps := 0
	for i := range f {
		if f[i] != d[i] {
			differing = append(differing, f[i][:strings.Index(f[i], " ")])
			if len(differing) <= 3 {
				t.Logf("report %d differs:\n flow   %s\n direct %s", i, f[i], d[i])
			}
		}
		m := documentLine.FindStringSubmatch(d[i])
		var n int
		fmt.Sscanf(m[2], "%d", &n)
		steps += n
		if m[4] != "0" {
			t.Errorf("the direct build's Document differs from a parse from scratch: %s", d[i])
		}
	}
	if steps < len(f)*2 {
		t.Fatalf("only %d steps in %d reports: the edits do not run", steps, len(f))
	}
	return differing
}

// documentShapes examine the input beyond the end of the result of their rule, which a Document must record to
// know when an edit invalidates the result: a lookahead over several characters, a negative lookahead and a
// lookahead over a class. The corpus grammars rarely look further than one character past a result.
var documentShapes = []genCase{
	{"positive lookahead", "rule main = item* $$\nrule item = \"a\" &\"bcd\" x / \"a\" y / \"b\" / \"c\" / \"d\"\nrule x = \"\"\nrule y = \"\"\n",
		[]string{"abcd", "abcdabce", "abcabcd"}},
	{"negative lookahead", "rule main = item* $$\nrule item = \"a\" !\"bc\" x / \"a\" y / [b-e]\nrule x = \"\"\nrule y = \"\"\n",
		[]string{"abcd", "abdabce", "aab"}},
	{"sequence lookahead", "rule main = item* $$\nrule item = \"a\" &(\"b\" [c-d] \"e\") x / \"a\" y / [b-e]\nrule x = \"\"\nrule y = \"\"\n",
		[]string{"abce", "abdeabce", "abcf"}},
	{"any-character lookahead", "rule main = item* $$\nrule item = \"a\" &(. . .) x / \"a\" y / [b-e] / \" \"\nrule x = \"\"\nrule y = \"\"\n",
		[]string{"abcd", "ab cdab", "a  "}},
	{"class lookahead", "rule main = item* $$\nrule item = [a-c] &[0-9] [0-9]+ / [a-c] / [0-9] / \" \"\n",
		[]string{"a1 b c2 3", "a b1", "c33a"}},
}

// documentCorpus returns the generated-corpus grammars and the shape grammars with the edits of each input.
func documentCorpus(t *testing.T) []documentCase {
	var cases []documentCase
	for _, c := range append(append(genCorpus(t), observeShapes...), documentShapes...) {
		cases = append(cases, documentCase{c, 300})
	}
	return cases
}

// TestFlowNodeDocumentsMatchDirect compares the Documents of the generated corpus.
func TestFlowNodeDocumentsMatchDirect(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	eachMode(t, func(t *testing.T) {
		f, d := runDocumentProbes(t, documentCorpus(t), "main", nil)
		if diff := documentDiffs(t, f, d); len(diff) > 0 {
			t.Errorf("%d reports differ between the Flow and the direct build: %v", len(diff), diff)
		}
	})
}

// TestFlowNodeDocumentsOnGrammars compares the Documents of the checked-in grammars on their testdata and
// operator inputs, with the default start rule.
func TestFlowNodeDocumentsOnGrammars(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the large parsers twice")
	}
	eachMode(t, func(t *testing.T) {
		var cases []documentCase
		for _, c := range grammarCases(t) {
			cases = append(cases, documentCase{c, 60})
		}
		f, d := runDocumentProbes(t, cases, "", nil)
		if diff := documentDiffs(t, f, d); len(diff) > 0 {
			t.Errorf("%d reports differ between the Flow and the direct build: %v", len(diff), diff)
		}
	})
}

var guardedRead = regexp.MustCompile(`p\.Document == nil && `)

// TestFlowNodeDocumentMutants checks that the comparison notices a Flow build that reads input in place in a
// Document parse: the mutants remove the guard of one in-place read (a literal, then a peek), or of all of them,
// so that the read does not record how far the parse examined; a Document then reuses results that an edit
// has invalidated, which differs from a parse from scratch and from the direct build. Verify rejects the same
// read in the IR (TestVerifyRejects in package flow).
func TestFlowNodeDocumentMutants(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	t.Setenv(modeEnv, "1")
	var cases []documentCase
	for _, c := range documentShapes {
		cases = append(cases, documentCase{c, 300})
	}
	f, d := runDocumentProbes(t, cases, "main", nil)
	if diff := documentDiffs(t, f, d); len(diff) > 0 {
		t.Fatalf("the unmutated builds differ: %v", diff)
	}
	for _, m := range []struct {
		name   string
		mutate func(string) string
	}{
		{"multi-character literal reads", func(s string) string { return strings.ReplaceAll(s, `p.Document == nil && (p.Pos+`, `(p.Pos+`) }},
		{"single-character literal reads", func(s string) string {
			return strings.ReplaceAll(s, `p.Document == nil && (p.Pos < len(p.In) && p.In[p.Pos] ==`, `(p.Pos < len(p.In) && p.In[p.Pos] ==`)
		}},
		{"peeks", func(s string) string {
			return strings.ReplaceAll(s, `if p.Document == nil && p.Pos < len(p.In) {`, `if p.Pos < len(p.In) {`)
		}},
		{"every read", func(s string) string { return guardedRead.ReplaceAllString(s, "") }},
	} {
		t.Run(m.name, func(t *testing.T) {
			changed := 0
			mf, _ := runDocumentProbes(t, cases, "main", func(code []byte) []byte {
				out := m.mutate(string(code))
				if out != string(code) {
					changed++
				}
				return []byte(out)
			})
			if changed == 0 {
				t.Fatalf("the mutant %q changes no build", m.name)
			}
			if mf == nil {
				return // the consumer of the mutant failed
			}
			if diff := documentDiffs(t, mf, d); len(diff) == 0 {
				t.Errorf("the mutant %q changes no report: the reads are not compared", m.name)
			}
		})
	}
}

var _ = flow.Verify
