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
func encodeTypedDecisions(w *strings.Builder, conds *typedDecisionsConds, mode, name string, dec *genDecisions) error {
	prog := dec.gen.prog
	all := append(append([]*rule(nil), prog.rules...), prog.twins...)
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
		if d.table != "trules" {
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
	var conds typedDecisionsConds
	var modes []string
	perMode := map[string]map[string]string{}
	names := map[string]bool{}
	records := 0
	for _, mode := range coreGenModes {
		modes = append(modes, mode.name)
		perMode[mode.name] = map[string]string{}
		grammars := typedDecisionsGrammars(t, mode)
		var gnames []string
		for name := range grammars {
			gnames = append(gnames, name)
		}
		sort.Strings(gnames) // the conditions are interned in this order, so it must be fixed
		for _, name := range gnames {
			dec := grammars[name]
			for _, d := range dec.log {
				if d.table == "trules" {
					records++
				}
			}
			var blk strings.Builder
			if err := encodeTypedDecisions(&blk, &conds, mode.name, name, dec); err != nil {
				t.Fatal(err)
			}
			perMode[mode.name][name] = blk.String()
			names[name] = true
		}
	}
	if records == 0 {
		t.Fatal("the generator records no typed decisions in this tree; regenerate from a tree that does")
	}
	if _, err := os.Stat(typedDecisionsGolden); err == nil {
		readTypedDecisions(t)
		var missing, extra []string
		for k := range typedDecisionsFile.lines {
			if _, ok := perMode[k.mode][k.grammar]; !ok {
				missing = append(missing, k.mode+" "+k.grammar)
			}
		}
		for _, m := range modes {
			for name := range perMode[m] {
				if _, ok := typedDecisionsFile.lines[typedDecisionsKey{m, name}]; !ok {
					extra = append(extra, m+" "+name)
				}
			}
		}
		if len(missing) != 0 || len(extra) != 0 {
			sort.Strings(missing)
			sort.Strings(extra)
			t.Fatalf("this tree records typed decisions for other grammars than %s: missing %v, extra %v; "+
				"regenerate from the tree the golden was written from", typedDecisionsGolden, missing, extra)
		}
	}
	var sorted []string
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	var b strings.Builder
	b.WriteString("# Typed lowering decisions of the Core pass comparisons (see core_typed_decisions_test.go).\n")
	for i, c := range conds.list {
		fmt.Fprintf(&b, "cond\t%d\t%s\n", i, strconv.Quote(c))
	}
	for _, name := range sorted {
		byBody := map[string][]string{}
		var bodies []string
		for _, m := range modes {
			blk, ok := perMode[m][name]
			if !ok {
				continue
			}
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
	t.Logf("wrote %d bytes, %d recorded typed decisions", b.Len(), records)
}

// typedDecisionsKey identifies the decisions of a grammar in a mode.
type typedDecisionsKey struct{ mode, grammar string }

// typedDecisionsGrammarsOf returns the names of the grammars that the golden file holds decisions
// for in the mode. The caller may modify the map.
func typedDecisionsGrammarsOf(t *testing.T, mode string) map[string]bool {
	t.Helper()
	readTypedDecisions(t)
	out := map[string]bool{}
	for k := range typedDecisionsFile.lines {
		if k.mode == mode {
			out[k.grammar] = true
		}
	}
	return out
}

// typedDecisionsFile is the golden file, read once: the lines of each mode and grammar, and the
// condition texts.
var typedDecisionsFile struct {
	loaded bool
	lines  map[typedDecisionsKey][]string
	conds  []string
}

// readTypedDecisions reads the golden file.
func readTypedDecisions(t *testing.T) {
	t.Helper()
	if typedDecisionsFile.loaded {
		return
	}
	typedDecisionsFile.loaded = true
	typedDecisionsFile.lines = map[typedDecisionsKey][]string{}
	f, err := os.Open(typedDecisionsGolden)
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
			typedDecisionsFile.conds = append(typedDecisionsFile.conds, c)
		case "grammar":
			g, err := strconv.Unquote(fields[2])
			if err != nil {
				t.Fatal(err)
			}
			block = block[:0]
			for _, m := range strings.Split(fields[1], ",") {
				k := typedDecisionsKey{m, g}
				block = append(block, k)
				typedDecisionsFile.lines[k] = nil
			}
		default:
			for _, k := range block {
				typedDecisionsFile.lines[k] = append(typedDecisionsFile.lines[k], line)
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
func loadTypedDecisions(t *testing.T, mode, name string, dec *genDecisions) {
	t.Helper()
	readTypedDecisions(t)
	lines, ok := typedDecisionsFile.lines[typedDecisionsKey{mode, name}]
	if !ok {
		t.Fatalf("%s %s: no typed decisions in %s", mode, name, typedDecisionsGolden)
	}
	// The golden replaces the typed entries the generator recorded itself, such as the Operators
	// Rule scopes that Flow IR records, so each comparison reads the frozen decisions only.
	kept := dec.log[:0]
	for _, d := range dec.log {
		if d.table != "trules" {
			kept = append(kept, d)
		}
	}
	dec.log = kept
	prog := dec.gen.prog
	all := append(append([]*rule(nil), prog.rules...), prog.twins...)
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
		x.table, x.rule, x.kind, x.e = "trules", all[id], kind, expr(id, at)
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
			if ci < 0 || ci >= len(typedDecisionsFile.conds) {
				t.Fatalf("%s %s: no condition %d", mode, name, ci)
			}
			add(atoi(f[1]), decGuard, atoi(f[2]), genDecision{guard: genGuard{
				ok: f[3] == "1", depth: atoi(f[4]), class: f[5] == "1", desc: expID(atoi(f[6])), cond: typedDecisionsFile.conds[ci],
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
