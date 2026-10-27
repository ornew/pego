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

// The typed lowering decisions of the corpus are frozen in testdata/typed_decisions.golden. The
// comparisons of the Core IR passes (core_gen_test.go, core_passes_test.go) read them from there,
// because the generator no longer records them: its typed Go output is printed from Flow IR, and
// the decisions that the typed direct rules recorded before that (dgen's typed paths and
// generator.expr) were removed with them. The golden was written from origin/main 82d0c805, whose
// generator records the same decisions as the commit before the Flow IR default (f805e8ab), by
// this same encoder.
//
// Regenerate the golden only when a change of a Core pass's decisions is intended and justified,
// and review the diff: a regenerated golden makes a broken pass pass. The writer records the
// typed decisions of the tree it runs in, so regeneration needs a tree whose generator records
// them as "trules" entries of dec.log for every grammar of the golden. The writer refuses to write
// when the grammars it records differ from the grammars of the existing golden, which a tree whose
// generator prints typed rules without recording them (Flow IR) does not match. It is driven by the
// environment:
//
//	PEGO_TYPED_DECISIONS_OUT=<absolute path> go test -run TestWriteTypedDecisions ./internal/engine/
//
// The file holds "cond" lines that define the guard conditions by index, then, for each grammar, a
// "grammar" line naming the modes whose decisions it is (modes with the same decisions share it),
// and the decision lines. Rules are identified by their ID, and expressions by their index in a
// walk of the rule's body (walkExpr), so the file does not depend on pointers.
const typedDecisionsGolden = "testdata/typed_decisions.golden"

// recognizerDecisionsGolden freezes the decisions of the recognizer table (recRules) in the same
// form, for the comparison of the Core IR passes with the recognizers (core_recognize_test.go).
// The direct printer that recorded them is not the printer of the recognizer any longer; the
// golden was written from it by the writer TestWriteRecognizerDecisions, driven by
//
//	PEGO_RECOGNIZER_DECISIONS_OUT=<absolute path> go test -run TestWriteRecognizerDecisions ./internal/engine/
//
// and the same rules apply as for the typed golden: regenerate only for an intended change of a
// Core pass's decisions, and review the diff.
const recognizerDecisionsGolden = "testdata/recognizer_decisions.golden"

// decisionsGolden is a golden file of the decisions of one rule table, read once: the lines of
// each mode and grammar, and the condition texts.
type decisionsGolden struct {
	path, table string
	loaded      bool
	lines       map[typedDecisionsKey][]string
	conds       []string
}

var (
	typedGolden      = &decisionsGolden{path: typedDecisionsGolden, table: "trules"}
	recognizerGolden = &decisionsGolden{path: recognizerDecisionsGolden, table: "recRules"}
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

// typedDecisionsGrammars returns the corpus grammars generated in the mode with their recorded
// decisions, as coreGenerations generates them, without reading the golden file.
func typedDecisionsGrammars(t *testing.T, mode coreGenMode) map[string]*genDecisions {
	t.Helper()
	out := map[string]*genDecisions{}
	for _, gc := range append(schemaCorpus(t), coreExtraGrammars...) {
		if dec, ok := coreGenerate(gc.src, mode.opts); ok && writesTyped(dec) {
			out[gc.name] = dec
		}
	}
	return out
}

// typedDecisionsConds interns the condition texts of the guards: each is written once, as a
// "cond" line, and a guard refers to it by index.
type typedDecisionsConds struct {
	index map[string]int
	list  []string
}

func (c *typedDecisionsConds) of(s string) int {
	if i, ok := c.index[s]; ok {
		return i
	}
	if c.index == nil {
		c.index = map[string]int{}
	}
	c.index[s] = len(c.list)
	c.list = append(c.list, s)
	return len(c.list) - 1
}

// typedDecisionsBit is the 0 or 1 that a boolean is written as.
func typedDecisionsBit(b bool) int {
	if b {
		return 1
	}
	return 0
}

// encodeTypedDecisions writes the typed decisions of the grammar as lines.
func encodeTypedDecisions(w *strings.Builder, conds *typedDecisionsConds, table string, all []*rule, mode, name string, dec *genDecisions) error {
	ids := map[*rule]int{}
	for i, r := range all {
		if r.id != i {
			return fmt.Errorf("%s %s: rule %s has ID %d at %d", mode, name, r.name, r.id, i)
		}
		ids[r] = i
	}
	ranges := map[int][]int{}
	walks := map[*rule]map[grammar.Expr]int{}
	for _, d := range dec.log {
		if d.table != table {
			continue
		}
		id, ok := ids[d.rule]
		if !ok {
			return fmt.Errorf("%s %s: decision rule %s not in the program", mode, name, d.rule.name)
		}
		eidx, ok := walks[d.rule]
		if !ok {
			_, eidx = typedDecisionsWalk(d.rule)
			walks[d.rule] = eidx
		}
		at, ok := eidx[d.e]
		if !ok {
			return fmt.Errorf("%s %s: rule %s: decision expression %s not in the body", mode, name, d.rule.name, grammar.FormatExpr(d.e))
		}
		switch d.kind {
		case decExpr:
			ranges[id] = append(ranges[id], at)
		case decTerminal:
			fmt.Fprintf(w, "terminal\t%d\t%d\n", id, at)
		case decGuard:
			g := d.guard
			fmt.Fprintf(w, "guard\t%d\t%d\t%d\t%d\t%d\t%d\t%d\n", id, at, typedDecisionsBit(g.ok), g.depth, typedDecisionsBit(g.class), int32(g.desc), conds.of(g.cond))
		case decClassRuns:
			var runs []string
			for _, r := range d.runs {
				runs = append(runs, fmt.Sprintf("%d:%d", r[0], r[1]))
			}
			if len(runs) == 0 {
				runs = []string{"-"}
			}
			fmt.Fprintf(w, "runs\t%d\t%d\t%s\n", id, at, strings.Join(runs, " "))
		case decScan:
			fmt.Fprintf(w, "scan\t%d\t%d\n", id, at)
		case decScope:
			fmt.Fprintf(w, "scope\t%d\t%d\t%s\n", id, at, strings.Join(d.names, ","))
		default:
			return fmt.Errorf("%s %s: unknown decision kind %d", mode, name, d.kind)
		}
	}
	var rs []int
	for id := range ranges {
		rs = append(rs, id)
	}
	sort.Ints(rs)
	for _, id := range rs {
		at := ranges[id]
		sort.Ints(at)
		var parts []string
		for i := 0; i < len(at); {
			j := i
			for j+1 < len(at) && at[j+1] <= at[j]+1 {
				j++
			}
			if at[i] == at[j] {
				parts = append(parts, strconv.Itoa(at[i]))
			} else {
				parts = append(parts, fmt.Sprintf("%d-%d", at[i], at[j]))
			}
			i = j + 1
		}
		fmt.Fprintf(w, "expr\t%d\t%s\n", id, strings.Join(parts, ","))
	}
	return nil
}

// TestWriteTypedDecisions writes the typed decisions of the corpus in every core mode to the file
// named by PEGO_TYPED_DECISIONS_OUT. It is skipped without that variable.
func TestWriteTypedDecisions(t *testing.T) {
	out := os.Getenv("PEGO_TYPED_DECISIONS_OUT")
	if out == "" {
		t.Skip("set PEGO_TYPED_DECISIONS_OUT to write the typed decisions golden")
	}
	writeDecisionsGolden(t, typedGolden, out, "# Typed lowering decisions of the Core pass comparisons (see core_typed_decisions_test.go).\n",
		coreGenModes, typedDecisionsGrammars, typedRules)
}

// writeDecisionsGolden writes the decisions of the golden's table, that the grammars of each mode
// recorded, to out. grammars returns the generated grammars of a mode, and rules the rules of the
// program a generated grammar's decisions index.
func writeDecisionsGolden(t *testing.T, gd *decisionsGolden, out, header string, modeList []coreGenMode,
	grammarsOf func(*testing.T, coreGenMode) map[string]*genDecisions, rules func(*genDecisions) []*rule) {
	t.Helper()
	var conds typedDecisionsConds
	var modes []string
	perMode := map[string]map[string]string{}
	names := map[string]bool{}
	records := 0
	for _, mode := range modeList {
		modes = append(modes, mode.name)
		perMode[mode.name] = map[string]string{}
		grammars := grammarsOf(t, mode)
		var gnames []string
		for name := range grammars {
			gnames = append(gnames, name)
		}
		sort.Strings(gnames) // the conditions are interned in this order, so it must be fixed
		for _, name := range gnames {
			dec := grammars[name]
			for _, d := range dec.log {
				if d.table == gd.table {
					records++
				}
			}
			var blk strings.Builder
			if err := encodeTypedDecisions(&blk, &conds, gd.table, rules(dec), mode.name, name, dec); err != nil {
				t.Fatal(err)
			}
			perMode[mode.name][name] = blk.String()
			names[name] = true
		}
	}
	if records == 0 {
		t.Fatalf("the generator records no %s decisions in this tree; regenerate from a tree that does", gd.table)
	}
	if _, err := os.Stat(gd.path); err == nil {
		gd.read(t)
		var missing, extra []string
		for k := range gd.lines {
			if _, ok := perMode[k.mode][k.grammar]; !ok {
				missing = append(missing, k.mode+" "+k.grammar)
			}
		}
		for _, m := range modes {
			for name := range perMode[m] {
				if _, ok := gd.lines[typedDecisionsKey{m, name}]; !ok {
					extra = append(extra, m+" "+name)
				}
			}
		}
		if len(missing) != 0 || len(extra) != 0 {
			sort.Strings(missing)
			sort.Strings(extra)
			t.Fatalf("this tree records %s decisions for other grammars than %s: missing %v, extra %v; "+
				"regenerate from the tree the golden was written from", gd.table, gd.path, missing, extra)
		}
	}
	var sorted []string
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	// The conditions are written in sorted order, so that a decision that adds one does not renumber the
	// others in the diff: perMode's guard lines refer to them by their first-use index, renumbered below.
	order := make([]int, len(conds.list)) // sorted position -> first-use index
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool { return conds.list[order[i]] < conds.list[order[j]] })
	renumber := make([]int, len(conds.list)) // first-use index -> sorted position
	for pos, old := range order {
		renumber[old] = pos
	}
	var cb strings.Builder
	for pos, old := range order {
		fmt.Fprintf(&cb, "cond\t%d\t%s\n", pos, strconv.Quote(conds.list[old]))
	}
	var b strings.Builder
	b.WriteString(header)
	b.WriteString(cb.String())
	for _, name := range sorted {
		byBody := map[string][]string{}
		var bodies []string
		for _, m := range modes {
			blk, ok := perMode[m][name]
			if !ok {
				continue
			}
			blk = renumberGuardConds(blk, renumber)
			if _, dup := byBody[blk]; !dup {
				bodies = append(bodies, blk)
			}
			byBody[blk] = append(byBody[blk], m)
		}
		for _, blk := range bodies {
			fmt.Fprintf(&b, "grammar\t%s\t%s\n", strings.Join(byBody[blk], ","), strconv.Quote(name))
			b.WriteString(blk)
		}
	}
	if err := os.WriteFile(out, []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %d bytes, %d recorded %s decisions", b.Len(), records, gd.table)
}

// renumberGuardConds replaces the condition index that ends each guard line of the block by the one at
// that position in renumber.
func renumberGuardConds(blk string, renumber []int) string {
	lines := strings.Split(blk, "\n")
	for i, l := range lines {
		f := strings.Split(l, "\t")
		if f[0] != "guard" {
			continue
		}
		n, err := strconv.Atoi(f[len(f)-1])
		if err != nil || n < 0 || n >= len(renumber) {
			panic("malformed guard line " + l)
		}
		f[len(f)-1] = strconv.Itoa(renumber[n])
		lines[i] = strings.Join(f, "\t")
	}
	return strings.Join(lines, "\n")
}

// typedDecisionsKey identifies the decisions of a grammar in a mode.
type typedDecisionsKey struct{ mode, grammar string }

// typedDecisionsGrammarsOf returns the names of the grammars that the golden file holds decisions
// for in the mode. The caller may modify the map.
func (gd *decisionsGolden) grammarsOf(t *testing.T, mode string) map[string]bool {
	t.Helper()
	gd.read(t)
	out := map[string]bool{}
	for k := range gd.lines {
		if k.mode == mode {
			out[k.grammar] = true
		}
	}
	return out
}

// readTypedDecisions reads the golden file.
func (gd *decisionsGolden) read(t *testing.T) {
	t.Helper()
	if gd.loaded {
		return
	}
	gd.loaded = true
	gd.lines = map[typedDecisionsKey][]string{}
	f, err := os.Open(gd.path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var block []typedDecisionsKey
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
				t.Fatal(err)
			}
			gd.conds = append(gd.conds, c)
		case "grammar":
			g, err := strconv.Unquote(fields[2])
			if err != nil {
				t.Fatal(err)
			}
			block = block[:0]
			for _, m := range strings.Split(fields[1], ",") {
				k := typedDecisionsKey{m, g}
				block = append(block, k)
				gd.lines[k] = nil
			}
		default:
			for _, k := range block {
				gd.lines[k] = append(gd.lines[k], line)
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
}

// loadTypedDecisions sets the typed decisions of the grammar in the mode on dec from the golden
// file: it appends them to dec.log as "trules" decisions, for the rules and expressions of dec's
// program.
func (gd *decisionsGolden) load(t *testing.T, all []*rule, mode, name string, dec *genDecisions) {
	t.Helper()
	gd.read(t)
	lines, ok := gd.lines[typedDecisionsKey{mode, name}]
	if !ok {
		t.Fatalf("%s %s: no %s decisions in %s", mode, name, gd.table, gd.path)
	}
	// The golden replaces the typed entries the generator recorded itself, such as the Operators
	// Rule scopes that Flow IR records, so each comparison reads the frozen decisions only.
	kept := dec.log[:0]
	for _, d := range dec.log {
		if d.table != gd.table {
			kept = append(kept, d)
		}
	}
	dec.log = kept
	exprsOf := map[int][]grammar.Expr{}
	expr := func(id, at int) grammar.Expr {
		if _, ok := exprsOf[id]; !ok {
			exprsOf[id], _ = typedDecisionsWalk(all[id])
		}
		if at < 0 || at >= len(exprsOf[id]) {
			t.Fatalf("%s %s: rule %d has no expression %d", mode, name, id, at)
		}
		return exprsOf[id][at]
	}
	add := func(id int, kind genDecisionKind, at int, x genDecision) {
		if id < 0 || id >= len(all) {
			t.Fatalf("%s %s: no rule %d", mode, name, id)
		}
		x.table, x.rule, x.kind, x.e = gd.table, all[id], kind, expr(id, at)
		dec.log = append(dec.log, x)
	}
	atoi := func(s string) int {
		n, err := strconv.Atoi(s)
		if err != nil {
			t.Fatalf("%s %s: %v", mode, name, err)
		}
		return n
	}
	for _, line := range lines {
		f := strings.Split(line, "\t")
		switch f[0] {
		case "expr":
			id := atoi(f[1])
			for _, p := range strings.Split(f[2], ",") {
				lo, hi, ranged := strings.Cut(p, "-")
				a := atoi(lo)
				b := a
				if ranged {
					b = atoi(hi)
				}
				for at := a; at <= b; at++ {
					add(id, decExpr, at, genDecision{})
				}
			}
		case "terminal":
			add(atoi(f[1]), decTerminal, atoi(f[2]), genDecision{})
		case "guard":
			ci := atoi(f[7])
			if ci < 0 || ci >= len(gd.conds) {
				t.Fatalf("%s %s: no condition %d", mode, name, ci)
			}
			add(atoi(f[1]), decGuard, atoi(f[2]), genDecision{guard: genGuard{
				ok: f[3] == "1", depth: atoi(f[4]), class: f[5] == "1", desc: expID(atoi(f[6])), cond: gd.conds[ci],
			}})
		case "runs":
			var runs [][2]int
			if f[3] != "-" {
				for _, p := range strings.Fields(f[3]) {
					a, b, _ := strings.Cut(p, ":")
					runs = append(runs, [2]int{atoi(a), atoi(b)})
				}
			}
			add(atoi(f[1]), decClassRuns, atoi(f[2]), genDecision{runs: runs})
		case "scan":
			add(atoi(f[1]), decScan, atoi(f[2]), genDecision{})
		case "scope":
			var names []string
			if f[3] != "" {
				names = strings.Split(f[3], ",")
			}
			add(atoi(f[1]), decScope, atoi(f[2]), genDecision{names: names})
		default:
			t.Fatalf("%s %s: unknown golden line %q", mode, name, line)
		}
	}
}

// typedRules returns the rules of the program of dec by ID, value-free twins included.
func typedRules(dec *genDecisions) []*rule {
	prog := dec.gen.prog
	return append(append([]*rule(nil), prog.rules...), prog.twins...)
}

// recognizerRules returns the rules of the recognizer program of dec by ID, value-free twins included.
func recognizerRules(dec *genDecisions) []*rule {
	rec, err := dec.gen.prog.recognizer()
	if err != nil {
		panic(err)
	}
	return append(append([]*rule(nil), rec.rules...), rec.twins...)
}

// recognizerGenModes are the modes in which the Core IR of the recognizers is compared.
var recognizerGenModes = []coreGenMode{
	{"lean", GenOptions{Recognize: true}},
	{"controlled", GenOptions{Recognize: true, WorkControls: true}},
}

// recognizerCorpus is the corpus grammars of the recognizer comparisons.
func recognizerCorpus(t *testing.T) []genCase {
	return append(append(schemaCorpus(t), coreExtraGrammars...), recognizerKnownDifferences...)
}

// recognizerKnownDifferences are grammars with the differences between Core IR and the recognizer
// that core_recognize_test.go excuses: a capture that no predicate reads builds nothing in Core IR,
// so the class it captures can be a guarded class where the generator's guard sees a capture, and a
// rule that recovers has a class guard in Core IR that the generator does not write.
var recognizerKnownDifferences = []genCase{
	{name: "uncaptured class", src: "rule main = (x:[a-z] \"1\" / \"b\") $$\n"},
	{name: "recovering rule", src: "rule main = x* $$\nrule x = (\"a\" \"b\") #recover(skip=[^;]*) / [0-9]\n"},
}

// TestWriteRecognizerDecisions writes the recognizer decisions of the corpus in each mode of
// recognizerGenModes to the file named by PEGO_RECOGNIZER_DECISIONS_OUT. It is skipped without it.
func TestWriteRecognizerDecisions(t *testing.T) {
	out := os.Getenv("PEGO_RECOGNIZER_DECISIONS_OUT")
	if out == "" {
		t.Skip("set PEGO_RECOGNIZER_DECISIONS_OUT to write the recognizer decisions golden")
	}
	writeDecisionsGolden(t, recognizerGolden, out, "# Recognizer lowering decisions of the Core pass comparisons (see core_typed_decisions_test.go).\n",
		recognizerGenModes, func(t *testing.T, mode coreGenMode) map[string]*genDecisions {
			grammars := map[string]*genDecisions{}
			for _, gc := range recognizerCorpus(t) {
				if dec, ok := coreGenerate(gc.src, mode.opts); ok {
					if _, err := dec.gen.prog.recognizer(); err == nil {
						grammars[gc.name] = dec
					}
				}
			}
			return grammars
		}, recognizerRules)
}
