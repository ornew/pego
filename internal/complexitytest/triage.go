package complexitytest

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/ornew/zeroparse/internal/engine"
)

// A TriageGrammar is a bundled grammar whose causes are sorted into triage groups.
type TriageGrammar struct {
	Name string
	// Path is the path of the grammar file from this package.
	Path string
}

// TriageGrammars are the grammars of parsers/ and examples/ that the classifier does not prove
// Linear from their default start rules.
var TriageGrammars = []TriageGrammar{
	{"cel", "../../parsers/cel/cel.zp"},
	{"csv", "../../parsers/csv/csv.zp"},
	{"cue", "../../parsers/cue/cue.zp"},
	{"duckdb", "../../parsers/duckdb/duckdb.zp"},
	{"golang", "../../parsers/golang/golang.zp"},
	{"python", "../../parsers/python/python.zp"},
	{"typescript", "../../parsers/typescript/typescript.zp"},
	{"xml", "../../parsers/xml/xml.zp"},
	{"yaml", "../../parsers/yaml/yaml.zp"},
	{"minilang", "../../examples/minilang/minilang.zp"},
	{"outline", "../../examples/outline/outline.zp"},
}

// LinearGrammars are the bundled grammars that the classifier proves Linear; the triage tests
// check that they stay so.
var LinearGrammars = []TriageGrammar{
	{"json", "../../parsers/json/json.zp"},
	{"calc", "../../examples/calculator/calc.zp"},
	{"calc_lr", "../../examples/calculator/calc_lr.zp"},
}

// A TriageGroup is a set of causes of one grammar's classification with one shape, the input
// families that exercise it and the item that it is assigned to. Every witness and every reason
// of a grammar belongs to exactly one group (see TestTriageCoverage).
type TriageGroup struct {
	Grammar string
	ID      string
	// Witness selects the witnesses (the causes of a polynomial or exponential class); otherwise
	// the group holds reasons for the class unknown.
	Witness bool
	// Rules, when not empty, restricts the group to causes reported in these rules.
	Rules []string
	// Except excludes the causes reported in these rules, which another group holds.
	Except []string
	// Pattern is a regular expression that the message of every cause of the group matches.
	Pattern string
	// Shape describes what the causes have in common.
	Shape string
	// Assign is the item that the group is assigned to, or Inherent.
	Assign string
	// Families are the adversarial input families derived from the witness paths of the group.
	Families []TriageFamily
	// Order is the measured order of the worst family on every backend.
	Order Order
	// Budget is the recommended budget for a group whose cost is inherent.
	Budget string
}

// Inherent marks a group whose super-linear cost is inherent in the language that the grammar
// accepts or in its result.
const Inherent = "inherent"

// A TriageFamily is an input family of a triage group: Build returns the input of size n.
type TriageFamily struct {
	Name  string
	Build func(n int) string
	// Sizes are the doubling sizes at which the family is measured.
	Sizes []int
	// Lenient accepts inputs that finish with syntax errors.
	Lenient bool
	// Order is the measured order of the family on every backend.
	Order Order
}

// Matches reports whether the cause c, a witness when witness is set, belongs to g.
func (g *TriageGroup) Matches(c engine.ComplexityCause, witness bool) bool {
	if witness != g.Witness {
		return false
	}
	if len(g.Rules) > 0 && !contains(g.Rules, c.Rule) {
		return false
	}
	if contains(g.Except, c.Rule) {
		return false
	}
	return regexp.MustCompile(g.Pattern).MatchString(c.Message)
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if x == y {
			return true
		}
	}
	return false
}

// doubling returns k sizes starting at n, each twice the one before.
func doubling(n, k int) []int {
	out := make([]int, k)
	for i := range out {
		out[i] = n << i
	}
	return out
}

// nest returns open repeated n times, then mid, then close repeated n times.
func nest(open, mid, close string) func(n int) string {
	return func(n int) string { return rep(open, n) + mid + rep(close, n) }
}

// list returns head, n copies of unit joined by sep, then tail. A unit may hold a %d verb for
// its index; a unit with a verb is a format.
func list(head, unit, sep, tail string) func(n int) string {
	return func(n int) string {
		var b strings.Builder
		b.WriteString(head)
		for i := range n {
			if i > 0 {
				b.WriteString(sep)
			}
			if strings.Contains(unit, "%") {
				fmt.Fprintf(&b, unit, i)
			} else {
				b.WriteString(unit)
			}
		}
		b.WriteString(tail)
		return b.String()
	}
}

// TriageGroups returns the triage table.
func TriageGroups() []TriageGroup {
	var gs []TriageGroup
	gs = append(gs, triageSmall()...)
	gs = append(gs, triageGolang()...)
	gs = append(gs, triageDuckdb()...)
	gs = append(gs, triagePython()...)
	gs = append(gs, triageTypescript()...)
	gs = append(gs, triageCue()...)
	gs = append(gs, triageYaml()...)
	return gs
}

// stairDepth returns the depth d of a staircase of lines indented 0 to d-1 spaces whose length,
// about d²/2, is about n, so that the input grows in proportion to n.
func stairDepth(n int) int { return int(math.Sqrt(float64(2 * n))) }
