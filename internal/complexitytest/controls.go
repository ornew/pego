package complexitytest

import "github.com/ornew/pego/internal/engine"

// The classes that the complexity classifier assigns to a start rule.
const (
	ClassLinear    = engine.ClassLinear
	ClassLinearEnv = engine.ClassLinearEnv
	ClassPoly      = engine.ClassPoly
	ClassExp       = engine.ClassExp
	ClassUnknown   = engine.ClassUnknown
)

// Control is a grammar family with a known growth order.
type Control struct {
	Name   string
	Family string // E0 to E10, or a name for a case outside the table
	Source string // PEGO source with the start rule main
	// Input builds the input of size n.
	Input func(n int) string
	// Sizes are n of the series n, 2n, 4n; ShortSizes are used with testing.Short.
	Sizes, ShortSizes []int
	// Order is the measured growth order of the budget spend.
	Order Order
	// Capped lets the 4n point reach Cap steps. Only Exp entries may be capped.
	Capped bool
	// Class is the class that the complexity classifier assigns to main (see TestControlClasses).
	Class engine.ComplexityClass
	// Positive marks a control that is linear and guards against a classifier or runtime that
	// reports growth where there is none.
	Positive bool
	// BecomesLinear marks a negative control that the memoization of left-recursion cycle members
	// makes linear in measurement, while its class stays one without a claim of linear cost; see
	// Controls.
	BecomesLinear bool
}

// Cap is the budget that stops an exponential control. It keeps the exponential cases short.
const Cap = 20_000_000

// becomesLinearLanded switches the controls marked BecomesLinear to their linear order: the
// runtime memoizes left-recursion cycle members.
const becomesLinearLanded = true

// parens is the input of E2 and E3: n open parentheses, "n", then n closing ones, each followed
// by "z".
func parens(n int) string { return rep("(", n) + "n" + rep(")z", n) }

func double(n int) []int { return []int{n, 2 * n, 4 * n} }

// Controls returns the controls table. The negative controls record growth that the engine has
// today: when a planned change makes one linear, its entry moves to the linear set by hand. The
// negative E2 control is linear in measurement through the becomesLinearLanded switch, with a
// class that claims no linear cost; E1, E7, E8 and E9 move when repetition runs are
// reused in batch parses; E6 and E10 stay quadratic. The rebinding variants of E6 are positive
// controls: a binding after the left call adds no growth, and the restart in E6 is the negative
// control. Each source is a grammar from the measured families of the
// grammar complexity design record; "main" is the start rule.
func Controls() []Control {
	cs := []Control{
		{
			Name: "E0 repetition after backtracking, safe", Family: "E0", Positive: true,
			Source: "rule main = (a / \"a\")* $$\nrule a = \"a\" \"c\"\n",
			Input:  func(n int) string { return rep("a", n) },
			Sizes:  double(1000), ShortSizes: double(250), Order: Linear, Class: ClassLinear,
		},
		{
			Name: "E1 repetition after backtracking", Family: "E1",
			Source: "rule main = (a / \"a\")* $$\nrule a = \"a\"* \"c\"\n",
			Input:  func(n int) string { return rep("a", n) },
			Sizes:  double(250), ShortSizes: double(100), Order: Poly(2), Class: ClassPoly,
		},
		{
			Name: "E7 nested scans, degree 3", Family: "E7",
			Source: "rule main = (a / \"a\")* $$\nrule a = (b / \"a\")* \"c\"\nrule b = \"a\"* \"d\"\n",
			Input:  func(n int) string { return rep("a", n) },
			Sizes:  double(50), ShortSizes: double(32), Order: Poly(3), Class: ClassPoly,
		},
		{
			Name: "E9 lookahead over scans", Family: "E9",
			Source: "rule main = (\"a\" !rest)* \"a\"* $$\nrule rest = \"a\"* \"c\"\n",
			Input:  func(n int) string { return rep("a", n) },
			Sizes:  double(250), ShortSizes: double(100), Order: Poly(2), Class: ClassPoly,
		},
		{
			Name: "E8 recovery skips", Family: "E8",
			Source: "rule main = (x / \"a\")* $$\nrule x = (y #recover(skip=[a]+)) \"c\"\nrule y = \"b\"\n",
			Input:  func(n int) string { return rep("a", n) },
			Sizes:  double(250), ShortSizes: double(100), Order: Poly(2), Class: ClassPoly,
		},
		{
			Name: "E6 right recursion, safe", Family: "E6", Positive: true,
			Source: "rule main = (e \"q\" / \"x\")* $$\nrule e = \"x\" e / \"x\"\n",
			Input:  func(n int) string { return rep("x", n) },
			Sizes:  double(500), ShortSizes: double(125), Order: Linear, Class: ClassLinear,
		},
		{
			Name: "E6 left recursion restarted", Family: "E6",
			Source: "rule main = (e \"q\" / \"x\")* $$\nrule e = e \"x\" / \"x\"\n",
			Input:  func(n int) string { return rep("x", n) },
			Sizes:  double(250), ShortSizes: double(100), Order: Poly(2), Class: ClassPoly,
		},
		{
			Name: "LR rebinding after the left call, growing value", Family: "E6 variant", Positive: true,
			Source: "var s string = \"a\"\nrule main = e $$\nrule e = e \"x\" {s := s + s} / \"x\" {len(s) > 0}\n",
			Input:  func(n int) string { return rep("x", n) },
			Sizes:  double(250), ShortSizes: double(100), Order: Linear, Class: ClassLinearEnv,
		},
		{
			Name: "LR rebinding after the left call, counter", Family: "E6 variant", Positive: true,
			Source: "var k int\nrule main = e $$\nrule e = e \"x\" {k := k + 1} / \"x\" {k >= 0}\n",
			Input:  func(n int) string { return rep("x", n) },
			Sizes:  double(250), ShortSizes: double(100), Order: Linear, Class: ClassLinearEnv,
		},
		{
			Name: "E10 Operators Rule restarted", Family: "E10",
			Source: "rule main = (e \"q\" / \"x\")* $$\nrule e = #operators {\n    operand \"x\"\n    level add { infix left \"\" }\n}\n",
			Input:  func(n int) string { return rep("x", n) },
			Sizes:  double(250), ShortSizes: double(100), Order: Poly(2), Class: ClassPoly,
		},
		{
			Name: "E2 cycle members behind a leader, safe", Family: "E2", Positive: true,
			Source: "rule main = c $$\nrule a = \"n\"\nrule c = \"(\" c \")\" \"y\" / \"(\" c \")\" \"z\" / a\n",
			Input:  parens, Sizes: double(64), ShortSizes: double(32), Order: Linear, Class: ClassLinear,
		},
		{
			Name: "E2 non-leader cycle members", Family: "E2", BecomesLinear: true,
			Source: "rule main = c $$\nrule a = c \"+\" / \"n\"\nrule c = \"(\" c \")\" \"y\" / \"(\" c \")\" \"z\" / a\n",
			Input:  parens, Sizes: double(4), ShortSizes: double(3), Order: Exp, Capped: true, Class: ClassUnknown,
		},
		{
			Name: "E3 environments, safe", Family: "E3", Positive: true,
			Source: "var v string\nrule main = r $$\nrule r = \"(\" {v := \"0\"} r \")\" \"y\" / \"(\" {v := \"1\"} r \")\" \"z\" / \"n\" {len(v) >= 0}\n",
			// len examines all of v, whose size the classifier bounds only by its value.
			Input: parens, Sizes: double(64), ShortSizes: double(32), Order: Linear, Class: ClassLinearEnv,
		},
		{
			Name: "E3 diverging environments", Family: "E3",
			Source: "var v string\nrule main = r $$\nrule r = \"(\" {v := v + \"0\"} r \")\" \"y\" / \"(\" {v := v + \"1\"} r \")\" \"z\" / \"n\" {len(v) >= 0}\n",
			Input:  parens, Sizes: double(3), ShortSizes: double(2), Order: Exp, Capped: true, Class: ClassExp,
		},
		{
			Name: "E4 self-growing variable", Family: "E4",
			Source: "var s string = \"a\"\nrule main = r $$\nrule r = \"x\" {s := s + s} r / {len(s) > 0}\n",
			Input:  func(n int) string { return rep("x", n) },
			Sizes:  double(4), ShortSizes: double(3), Order: Exp, Capped: true, Class: ClassExp,
		},
		{
			Name: "E5 self-growing fold", Family: "E5",
			Source: "rule main = xs:\"x\"* => len(foldl(\"a\", $xs, (acc, x) => acc + acc))\n",
			Input:  func(n int) string { return rep("x", n) },
			Sizes:  double(4), ShortSizes: double(3), Order: Exp, Capped: true, Class: ClassExp,
		},
	}
	for i := range cs {
		if cs[i].BecomesLinear && becomesLinearLanded {
			// The growth becomes linear, but the classifier counts cycle members as not memoized,
			// since they are memoized only outside the growths they read: its class stays the one
			// it reports, which claims no linear cost, so the control is not positive.
			cs[i].Order, cs[i].Capped = Linear, false
			// A series of a linear control needs sizes where fixed cost no longer dominates: the
			// slopes of the E2 positive control are 0.98 and 0.99 from 64 and 0.97 and 0.98 from 32.
			cs[i].Sizes, cs[i].ShortSizes = double(64), double(32)
		}
	}
	return cs
}
