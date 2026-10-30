package gogen_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

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
	steps, bad, firstBad, reused := 0, 0, -1, 0
	alpha := []rune(in.Alpha)
	state := uint64(len(in.Text))*2654435761 + 12345
	next := func(n int) int {
		state = state*6364136223846793005 + 1442695040888963407
		return int((state >> 33) % uint64(n))
	}
	d, err := e.newDoc(in.Text)
	if err != nil {
		return fmt.Sprintf("%s steps=0 digest=%q bad=0 first=-1 reused=0", id, err.Error())
	}
	observe := func() {
		for k := 0; k < 2; k++ {
			n, err := d.parse()
			st := d.stats()
			if k == 0 && steps > 0 && st.Reused > 0 {
				reused++
			}
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
	return fmt.Sprintf("%s steps=%d digest=%x bad=%d first=%d reused=%d", id, steps, h.Sum(nil)[:8], bad, firstBad, reused)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
`

var documentLine = regexp.MustCompile(`^(\S+) steps=(\d+) digest=(\S+) bad=(\d+) first=(-?\d+) reused=(\d+)$`)

type documentInput struct {
	Text  string
	Edits int
	Alpha string
}

// documentCase is a grammar with the inputs of its Documents and the number of edits applied to each.
type documentCase struct {
	gentest.Case
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
	for _, in := range c.Inputs {
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
	for _, in := range append([]string{""}, c.Inputs...) {
		out = append(out, documentInput{in, c.edits, string(alpha)})
	}
	return out
}

// runDocumentProbes builds the cases as the packages f0..fN with Documents (mutated by mutate if not nil) and
// returns the report line of the consumer for each input of each; the lines are nil if the consumer fails and
// mutate is set.
func runDocumentProbes(t *testing.T, cases []documentCase, start string, mutate func([]byte) []byte) []string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", gentest.GoMod("docprobe"))
	var inputs [][]documentInput
	var imports, entries strings.Builder
	for i, c := range cases {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: fmt.Sprintf("f%d", i), Start: start, Recognize: true, Document: true})
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if mutate != nil {
			code = mutate(code)
		}
		write(fmt.Sprintf("f%d/parser.go", i), string(code))
		inputs = append(inputs, documentInputs(c))
		fmt.Fprintf(&imports, "\tf%d \"docprobe/f%d\"\n", i, i)
		fmt.Fprintf(&entries, `	{newDoc: func(s string) (*doc, error) {
		d, err := f%[1]d.NewDocument(s)
		if err != nil {
			return nil, err
		}
		return &doc{func() (*pegort.Node, error) { return d.Parse() }, d.Edit, d.Text, d.Stats}, nil
	}, parse: func(s string) (*pegort.Node, error) { return f%[1]d.Parse(s) }},
`, i)
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
		if mutate != nil {
			return nil
		}
		t.Fatalf("go run: %v\n%s", err, stderr.String())
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		if !documentLine.MatchString(l) {
			t.Fatalf("go run: malformed probe line %q", l)
		}
		lines = append(lines, l)
	}
	return lines
}

const documentGolden = "testdata/document.golden"

const documentGoldenHeader = "# Digests of the Document parses of the generated Node parsers (results, errors, evaluation and reuse counts after\n" +
	"# every edit of a seeded sequence), by group, mode and case (see gogen_flow_document_test.go).\n" +
	"# Rewrite with: go test ./internal/gen/gogen -run Document -update\n" + gentest.GoldenRules

var documentGoldenFile = gentest.GoldenFile{Path: documentGolden, Header: documentGoldenHeader}

// documentDigests returns, for each case, the hash of its report lines, keyed by group/mode/name.
func documentDigests(t testing.TB, group string, cases []documentCase, lines []string) map[string]string {
	hashes := make([]hash.Hash, len(cases))
	for i := range hashes {
		hashes[i] = sha256.New()
	}
	for _, l := range lines {
		var i int
		fmt.Sscanf(l, "%d/", &i)
		_, rest, _ := strings.Cut(l, "/") // the case number is not part of the digest
		io.WriteString(hashes[i], rest+"\n")
	}
	out := map[string]string{}
	for i, c := range cases {
		out[fmt.Sprintf("%s/%s/%s", group, gentest.FixtureForm(t), c.Name)] = fmt.Sprintf("%x", hashes[i].Sum(nil)[:12])
	}
	return out
}

// checkDocumentReports checks the invariants of the reports (every Document result is a parse from scratch's,
// parses after edits reuse results and the edits run) and returns their
// digests.
func checkDocumentReports(t *testing.T, group string, cases []documentCase, lines []string) {
	t.Helper()
	if len(lines) == 0 {
		t.Fatal("no reports")
	}
	steps, reused := 0, 0
	for _, l := range lines {
		m := documentLine.FindStringSubmatch(l)
		var n, r int
		fmt.Sscanf(m[2], "%d", &n)
		fmt.Sscanf(m[6], "%d", &r)
		steps += n
		reused += r
		if m[4] != "0" {
			t.Errorf("a Document differs from a parse from scratch: %s", l)
		}
	}
	if steps < len(lines)*2 {
		t.Fatalf("only %d steps in %d reports: the edits do not run", steps, len(lines))
	}
	if reused == 0 {
		t.Fatal("no parse after an edit reuses a result")
	}
	documentGoldenFile.Check(t, group+"/"+gentest.FixtureForm(t)+"/", documentDigests(t, group, cases, lines))
}

// documentShapes examine the input beyond the end of the result of their rule, which a Document must record to
// know when an edit invalidates the result: a lookahead over several characters, a negative lookahead and a
// lookahead over a class. The corpus grammars rarely look further than one character past a result.
var documentShapes = []gentest.Case{
	{Name: "positive lookahead", Src: "rule main = item* $$\nrule item = \"a\" &\"bcd\" x / \"a\" y / \"b\" / \"c\" / \"d\"\nrule x = \"\"\nrule y = \"\"\n",
		Inputs: []string{"abcd", "abcdabce", "abcabcd"}},
	{Name: "negative lookahead", Src: "rule main = item* $$\nrule item = \"a\" !\"bc\" x / \"a\" y / [b-e]\nrule x = \"\"\nrule y = \"\"\n",
		Inputs: []string{"abcd", "abdabce", "aab"}},
	{Name: "sequence lookahead", Src: "rule main = item* $$\nrule item = \"a\" &(\"b\" [c-d] \"e\") x / \"a\" y / [b-e]\nrule x = \"\"\nrule y = \"\"\n",
		Inputs: []string{"abce", "abdeabce", "abcf"}},
	{Name: "any-character lookahead", Src: "rule main = item* $$\nrule item = \"a\" &(. . .) x / \"a\" y / [b-e] / \" \"\nrule x = \"\"\nrule y = \"\"\n",
		Inputs: []string{"abcd", "ab cdab", "a  "}},
	{Name: "class lookahead", Src: "rule main = item* $$\nrule item = [a-c] &[0-9] [0-9]+ / [a-c] / [0-9] / \" \"\n",
		Inputs: []string{"a1 b c2 3", "a b1", "c33a"}},
}

// documentCorpus returns the generated-corpus grammars and the shape grammars with the edits of each input.
func documentCorpus(t *testing.T) []documentCase {
	var cases []documentCase
	for _, c := range append(append(gentest.GenCorpus(t), observeShapes...), documentShapes...) {
		cases = append(cases, documentCase{c, 300})
	}
	return cases
}

// TestFlowNodeDocumentsReuseAndMatchScratch checks the Documents of the generated corpus.
func TestFlowNodeDocumentsReuseAndMatchScratch(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	gentest.EachMode(t, func(t *testing.T) {
		cases := documentCorpus(t)
		checkDocumentReports(t, "corpus", cases, runDocumentProbes(t, cases, "main", nil))
	})
}

// TestFlowNodeDocumentsOnGrammars checks the Documents of the checked-in grammars on their testdata and
// operator inputs, with the default start rule.
func TestFlowNodeDocumentsOnGrammars(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds the large parsers")
	}
	gentest.EachMode(t, func(t *testing.T) {
		var cases []documentCase
		for _, c := range gentest.GrammarCases(t) {
			cases = append(cases, documentCase{c, 60})
		}
		checkDocumentReports(t, "grammars", cases, runDocumentProbes(t, cases, "", nil))
	})
}

var guardedRead = regexp.MustCompile(`p\.Document == nil && `)

// TestFlowNodeDocumentMutants checks that the checks notice a Flow build that reads input in place in a
// Document parse: the mutants remove the guard of one in-place read (a literal, then a peek), or of all of them,
// so that the read does not record how far the parse examined; a Document then reuses results that an edit
// has invalidated, which differs from a parse from scratch or changes the counts the golden holds. Verify
// rejects the same read in the IR (TestVerifyRejects in package flow).
func TestFlowNodeDocumentMutants(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	gentest.PinOrdinary(t)
	var cases []documentCase
	for _, c := range documentShapes {
		cases = append(cases, documentCase{c, 300})
	}
	golden := gentest.ReadGolden(t, documentGolden)
	digestsDiffer := func(lines []string) bool {
		for k, v := range documentDigests(t, "corpus", cases, lines) {
			if w, ok := golden[k]; !ok || w != v {
				return true
			}
		}
		return false
	}
	base := runDocumentProbes(t, cases, "main", nil)
	if digestsDiffer(base) {
		t.Fatalf("the digests of the unmutated build differ from %s", documentGolden)
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
			mf := runDocumentProbes(t, cases, "main", func(code []byte) []byte {
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
			stale := false
			for _, l := range mf {
				if mm := documentLine.FindStringSubmatch(l); mm[4] != "0" {
					stale = true
				}
			}
			if !stale && !digestsDiffer(mf) {
				t.Errorf("the mutant %q changes no report: the reads are not checked", m.name)
			}
		})
	}
}
