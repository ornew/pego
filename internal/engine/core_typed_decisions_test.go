package engine

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

// The lowering decisions of the Core IR passes over the corpus are frozen in two golden files:
// testdata/typed_decisions.golden for the typed Go rules (table trules) and
// testdata/recognizer_decisions.golden for the recognizers (recRules). They are Core's own
// decisions, as coreDecisionLines encodes them: the Terminals, the guard of every alternative,
// the class runs, the scans, the scopes and, for the typed rules, the structure and forms of
// every action, predicate and binding. A run of TestCoreTypedDecisionsFrozen or
// TestCoreRecognizerDecisionsFrozen compares the Core IR of every corpus grammar with its golden
// entry; the negative controls of core_passes_test.go and core_terms_test.go break a pass and
// require the comparison to notice.
//
// A golden is a regression freeze, not an oracle: regenerating it makes a changed decision pass.
// Regenerate it only for a change of a pass's decisions that is intended and justified, and review
// the diff. The run prints the changed, added and removed grammars per mode and their counts:
//
//	go test ./internal/engine -run 'Test(CoreTypedDecisionsFrozen|CoreRecognizerDecisionsFrozen)$' -update
//
// Run it on its own: the other tests of the Core passes read the golden as it was when they began.
//
// The file holds "cond" lines that define the guard conditions by index (in sorted order, so that
// a new condition does not renumber the others), then, for each grammar, a "grammar" line naming
// the modes whose decisions it holds (modes with the same decisions share it), and the decision
// lines. Rules are identified by their ID, and expressions by their index in a walk of the rule's
// body (typedDecisionsWalk), so the file does not depend on pointers:
//
//	terminal rule expr
//	guard    rule expr ok depth class expectation cond
//	runs     rule expr first:length ...
//	scan     rule expr
//	scope    rule expr slot,...
//	term     rule at tree           at is the expression of a predicate or binding, -1 for the
//	                                action of the rule and -2-i for the action of line i
const (
	typedDecisionsGolden      = "testdata/typed_decisions.golden"
	recognizerDecisionsGolden = "testdata/recognizer_decisions.golden"
)

// decisionsKey identifies the decisions of a grammar in a mode.
type decisionsKey struct{ mode, grammar string }

// decisionsGolden is a golden file of decisions, read once: the sorted lines of each mode and
// grammar, with the guard conditions as quoted text.
type decisionsGolden struct {
	path, header string
	loaded       bool
	lines        map[decisionsKey][]string
}

var (
	typedGolden = &decisionsGolden{path: typedDecisionsGolden,
		header: "# Typed lowering decisions of the Core IR passes (see core_typed_decisions_test.go).\n"}
	recognizerGolden = &decisionsGolden{path: recognizerDecisionsGolden,
		header: "# Recognizer lowering decisions of the Core IR passes (see core_typed_decisions_test.go).\n"}
)

// typedDecisionsWalk returns the expressions of the rule's body in walk order, and their indexes.
func typedDecisionsWalk(r *rule) ([]grammar.Expr, map[grammar.Expr]int) {
	var exprs []grammar.Expr
	idx := map[grammar.Expr]int{}
	if e := r.bodyExpr(); e != nil {
		walkExpr(e, func(x grammar.Expr) {
			if _, ok := idx[x]; !ok {
				idx[x] = len(exprs)
			}
			exprs = append(exprs, x)
		})
	}
	return exprs, idx
}

// typedDecisionsBit is the 0 or 1 that a boolean is written as.
func typedDecisionsBit(b bool) int {
	if b {
		return 1
	}
	return 0
}

// swapLastField replaces the last tab-separated field of the line.
func swapLastField(line, last string) string {
	i := strings.LastIndexByte(line, '\t')
	return line[:i+1] + last
}

// read reads the golden file, once. It panics if the file is malformed.
func (gd *decisionsGolden) read() {
	if gd.loaded {
		return
	}
	gd.loaded = true
	gd.lines = map[decisionsKey][]string{}
	f, err := os.Open(gd.path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	var conds []string
	var block []decisionsKey
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.SplitN(line, "\t", 4)
		switch fields[0] {
		case "cond":
			c, err := strconv.Unquote(fields[2])
			if err != nil {
				panic(err)
			}
			conds = append(conds, c)
		case "grammar":
			g, err := strconv.Unquote(fields[2])
			if err != nil {
				panic(err)
			}
			block = block[:0]
			for _, m := range strings.Split(fields[1], ",") {
				k := decisionsKey{m, g}
				block = append(block, k)
				gd.lines[k] = nil
			}
		default:
			if fields[0] == "guard" {
				i := strings.LastIndexByte(line, '\t')
				n, err := strconv.Atoi(line[i+1:])
				if err != nil || n < 0 || n >= len(conds) {
					panic("malformed guard line " + line)
				}
				line = swapLastField(line, strconv.Quote(conds[n]))
			}
			for _, k := range block {
				gd.lines[k] = append(gd.lines[k], line)
			}
		}
	}
	if err := sc.Err(); err != nil {
		panic(err)
	}
	for _, ls := range gd.lines {
		sort.Strings(ls)
	}
}

// block is the text of the lines of a grammar with the conditions replaced by their indexes.
func decisionsBlock(lines []string, condIndex map[string]int) string {
	var b strings.Builder
	for _, l := range lines {
		if strings.HasPrefix(l, "guard\t") {
			i := strings.LastIndexByte(l, '\t')
			l = swapLastField(l, strconv.Itoa(condIndex[l[i+1:]]))
		}
		b.WriteString(l + "\n")
	}
	return b.String()
}

// write writes the golden with the decisions of every grammar of every mode in modes, in sorted
// order, and logs the grammars that changed against the golden it replaces.
func (gd *decisionsGolden) write(t *testing.T, modes []string, fresh map[string]map[string][]string) {
	t.Helper()
	old := map[decisionsKey]string{}
	if _, err := os.Stat(gd.path); err == nil {
		gd.read()
		for k, ls := range gd.lines {
			old[k] = strings.Join(ls, "\n")
		}
	}
	condSet := map[string]bool{}
	names := map[string]bool{}
	for _, m := range modes {
		for name, ls := range fresh[m] {
			names[name] = true
			for _, l := range ls {
				if strings.HasPrefix(l, "guard\t") {
					condSet[l[strings.LastIndexByte(l, '\t')+1:]] = true
				}
			}
		}
	}
	var conds []string
	for c := range condSet {
		conds = append(conds, c)
	}
	sort.Strings(conds)
	condIndex := map[string]int{}
	var b strings.Builder
	b.WriteString(gd.header)
	for i, c := range conds {
		condIndex[c] = i
		fmt.Fprintf(&b, "cond\t%d\t%s\n", i, c)
	}
	var sorted []string
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	grammars := 0
	for _, name := range sorted {
		byBody := map[string][]string{}
		var bodies []string
		for _, m := range modes {
			ls, ok := fresh[m][name]
			if !ok {
				continue
			}
			blk := decisionsBlock(ls, condIndex)
			if _, dup := byBody[blk]; !dup {
				bodies = append(bodies, blk)
			}
			byBody[blk] = append(byBody[blk], m)
			grammars++
		}
		for _, blk := range bodies {
			fmt.Fprintf(&b, "grammar\t%s\t%s\n", strings.Join(byBody[blk], ","), strconv.Quote(name))
			b.WriteString(blk)
		}
	}
	if err := os.WriteFile(gd.path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	var changed, added, removed []string
	for _, m := range modes {
		for name, ls := range fresh[m] {
			k := decisionsKey{m, name}
			was, ok := old[k]
			switch {
			case !ok:
				added = append(added, m+" | "+name)
			case was != strings.Join(ls, "\n"):
				changed = append(changed, m+" | "+name)
			}
		}
		for k := range old {
			if _, ok := fresh[k.mode][k.grammar]; k.mode == m && !ok {
				removed = append(removed, m+" | "+k.grammar)
			}
		}
	}
	list := func(label string, xs []string) {
		sort.Strings(xs)
		more := ""
		if len(xs) > 40 {
			xs, more = xs[:40], fmt.Sprintf(" ... and %d more", len(xs)-40)
		}
		t.Logf("update %s: %s %d: %s%s", gd.path, label, len(xs), strings.Join(xs, "; "), more)
	}
	t.Logf("update %s: %d grammar entries in %d modes, %d conditions; %d changed, %d added, %d removed", gd.path, grammars, len(modes), len(conds), len(changed), len(added), len(removed))
	if len(changed) > 0 {
		list("changed", changed)
	}
	if len(added) > 0 {
		list("added", added)
	}
	if len(removed) > 0 {
		list("removed", removed)
	}
}

// freeze runs one frozen-decisions test over the cases of each mode: it compares the cases with the
// golden (compareCoreDecisions) and the grammars of the golden with the cases (a missing or a stale
// entry fails), or with -update rewrites the golden from the cases. cases builds the Core IR of the
// grammars of a mode; check is run for every mode afterwards with the counts of its decisions.
func (gd *decisionsGolden) freeze(t *testing.T, modes []coreGenMode, cases func(t *testing.T, mode coreGenMode) []*coreGenCase, check func(t *testing.T, mode coreGenMode, n *coreDecisionCounts)) {
	fresh := map[string]map[string][]string{}
	var names []string
	for _, mode := range modes {
		names = append(names, mode.name)
		t.Run(mode.name, func(t *testing.T) {
			fresh[mode.name] = map[string][]string{}
			var n coreDecisionCounts
			for _, c := range cases(t, mode) {
				for _, err := range compareCoreDecisions(c, &n) {
					t.Errorf("%s: %s", c.name, err)
				}
				fresh[mode.name][c.name] = c.lines
			}
			if !*updateGoldens {
				gd.read()
				for k := range gd.lines {
					if _, ok := fresh[mode.name][k.grammar]; k.mode == mode.name && !ok {
						t.Errorf("%s: %s: stale entry in %s", mode.name, k.grammar, gd.path)
					}
				}
			}
			check(t, mode, &n)
		})
	}
	if *updateGoldens {
		if len(fresh) != len(modes) || t.Failed() {
			t.Fatalf("not rewriting %s: the run did not complete in every mode", gd.path)
		}
		gd.write(t, names, fresh)
	}
}

// typedRules returns the rules of the program of the generator by ID, value-free twins included.
func typedRules(g *generator) []*rule {
	return append(append([]*rule(nil), g.prog.rules...), g.prog.twins...)
}

// recognizerRules returns the rules of the recognizer program of the generator by ID, value-free
// twins included.
func recognizerRules(g *generator) []*rule {
	rec, err := g.prog.recognizer()
	if err != nil {
		panic(err)
	}
	return append(append([]*rule(nil), rec.rules...), rec.twins...)
}
