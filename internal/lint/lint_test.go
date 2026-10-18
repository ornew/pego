package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/syntax"
)

// lintSource lints src (which must compile) from the rule main and returns the findings as
// "line:col severity check".
func lintSource(t *testing.T, src string, opts Options) ([]string, []Finding) {
	t.Helper()
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Compile(g, engine.Options{}); err != nil {
		t.Fatalf("the test grammar does not compile: %v", err)
	}
	fs := Run(g, "main", opts)
	var out []string
	for _, f := range fs {
		out = append(out, fmt.Sprintf("%d:%d %s %s", f.Pos.Line, f.Pos.Col, f.Severity, f.Check))
	}
	return out, fs
}

type lintCase struct {
	name string
	src  string
	want []string // "line:col severity check", in order
	// msg, if set, must appear in the message of the first finding.
	msg string
}

func runCases(t *testing.T, cases []lintCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, fs := lintSource(t, tc.src, Options{})
			if !slices.Equal(got, tc.want) {
				var msgs []string
				for _, f := range fs {
					msgs = append(msgs, f.String())
				}
				t.Fatalf("got  %q\nwant %q\n%s", got, tc.want, strings.Join(msgs, "\n"))
			}
			if tc.msg != "" && !strings.Contains(fs[0].Message, tc.msg) {
				t.Errorf("message %q does not contain %q", fs[0].Message, tc.msg)
			}
		})
	}
}

func TestShadowedAlternative(t *testing.T) {
	runCases(t, []lintCase{
		{name: "literal prefix", src: `rule main = "a" / "ab"`,
			want: []string{"1:19 error shadowed-alternative"}, msg: "alternative 2 (`\"ab\"`) can never match: alternative 1"},
		{name: "longer literal first", src: `rule main = "ab" / "a"`},
		{name: "operator prefix", src: `rule main = "-" / "->" / "+"`,
			want: []string{"1:19 error shadowed-alternative"}},
		{name: "literal through a rule", src: "rule main = op / \"==\"\nrule op = \"=\"",
			want: []string{"1:18 error shadowed-alternative"}},
		{name: "keyword after identifier", src: "rule main = ident / \"if\"\nrule ident = [a-z]+ #atomic",
			want: []string{"1:21 error shadowed-alternative"}},
		{name: "keyword after identifier with a boundary", src: "rule main = ident / \"if\" ![a-z0-9]\nrule ident = [a-z]+ #atomic",
			want: []string{"1:21 error shadowed-alternative"}},
		{name: "keyword before identifier", src: "rule main = \"if\" ![a-z] / ident\nrule ident = [a-z]+ #atomic"},
		{name: "keyword without boundary before identifier", src: "rule main = kw / ident\nrule kw = \"if\" ![a-z]\nrule ident = [a-z]+ #atomic"},
		{name: "same items first", src: "rule main = x / x \"(\" \")\"\nrule x = \"x\"",
			want: []string{"1:17 error shadowed-alternative"}, msg: "matches first wherever it could"},
		{name: "same items with an optional tail", src: "rule main = x y? / x \"(\"\nrule x = \"x\"\nrule y = \"y\"",
			want: []string{"1:20 error shadowed-alternative"}},
		{name: "same items, then a longer literal", src: "rule main = x \".\" / x \"..\"\nrule x = \"x\"",
			want: []string{"1:21 error shadowed-alternative"}},
		{name: "longer alternative first", src: "rule main = x \"(\" \")\" / x\nrule x = \"x\""},
		{name: "duplicate", src: `rule main = "a" / "b" / "a"`,
			want: []string{"1:25 error shadowed-alternative"}, msg: "it is the same as alternative 1"},
		{name: "capture names do not matter", src: `rule main = a:"x" "y" / b:"x" "y"`,
			want: []string{"1:25 error shadowed-alternative"}},
		{name: "capture names matter to predicates", src: `rule main = a:"x" {len($a) > 1} / b:"x" {len($b) > 2}`},
		{name: "predicates are not certain", src: `rule main = a:"x" {len($a) > 1} / "x"`},
		{name: "always succeeds", src: `rule main = "a"? / "b" / "c"`,
			want: []string{"1:20 error shadowed-alternative"}, msg: "alternatives 2 to 3 are never tried: alternative 1"},
		{name: "always succeeds, one after", src: `rule main = "a"* / "b"`,
			want: []string{"1:20 error shadowed-alternative"}, msg: "alternative 2 (`\"b\"`) is never tried"},
		{name: "always succeeds at the end", src: `rule main = "a" / _`},
		// While r grows its left recursion, the call of r returns what it has found so far: at
		// first, a failure. "a" is tried then.
		{name: "left-recursive call", src: "rule main = r\nrule r = r / \"a\""},
		{name: "left-recursive call, then something else", src: "rule main = r\nrule r = r \"x\" / \"a\" / \"a\"",
			want: []string{"2:24 error shadowed-alternative"}},
		{name: "a cut can make an optional fail", src: `rule main = ("a" #cut "b")? / "c"`},
		{name: "a cut inside a choice stays there", src: `rule main = ("a" #cut "b" / "x")? / "c"`,
			want: []string{"1:37 error shadowed-alternative"}},
		{name: "a cut can make the choice fail first", src: `rule main = ("a" #cut "b" / "a"? "c") / "d"`},
		{name: "recovery is not a prefix", src: `rule main = ("x" "y") #recover(skip=[^;]+) / "z"`},
		{name: "explicit failure with a message", src: `rule main = "hello" / _|_ @error(message="expected hello")`},
		{name: "nested choice", src: `rule main = "(" ("<" / "<=") ")"`,
			want: []string{"1:24 error shadowed-alternative"}},
		{name: "operatorsRule operands", src: "rule main = operators {\n    operand \"a\"\n    operand \"ab\"\n    level { infix left \"+\" }\n}",
			want: []string{"3:13 error shadowed-alternative"}, msg: "operand 2"},
		{name: "operatorsRule duplicate operator", src: "rule main = operators {\n    operand [0-9]\n    level { infix left \"+\"; infix left \"+\" }\n}",
			want: []string{"3:29 error shadowed-alternative"}, msg: "is never selected"},
		{name: "operatorsRule operators of different kinds", src: "rule main = operators {\n    operand [0-9]\n    level { prefix \"-\"; infix left \"-\" }\n}"},
		{name: "operatorsRule longest match", src: "rule main = operators {\n    operand [0-9]\n    level { infix left \"<\"; infix left \"<=\" }\n}"},
		{name: "operatorsRule operators on levels a call can leave out",
			src: "rule main = level(e, b) \"!\"\nrule e = operators {\n    operand [0-9]\n    level a { infix left \"+\" }\n    level b { infix left \"+\" }\n}"},
		{name: "operatorsRule operators on levels no call leaves out",
			src:  "rule main = e \"!\"\nrule e = operators {\n    operand [0-9]\n    level a { infix left \"+\" }\n    level b { infix left \"+\" }\n}",
			want: []string{"5:15 error shadowed-alternative"}},
	})
}

func TestNeverMatches(t *testing.T) {
	runCases(t, []lintCase{
		{name: "end of input before input", src: `rule main = "a" $$ "b"`, want: []string{"1:17 error never-matches"}},
		{name: "end of input before nullable", src: `rule main = "a" $$ "b"*`},
		{name: "end of input before a lookahead for input", src: `rule main = "a" $$ &.`, want: []string{"1:17 error never-matches"}},
		{name: "end of input before a lookahead for the end", src: `rule main = "a" $$ !.`},
		{name: "beginning of input after input", src: `rule main = "a" ^^ "b"`, want: []string{"1:17 error never-matches"}},
		{name: "beginning of input after nullable", src: `rule main = "a"? ^^ "b"`},
		{name: "end of line before a character", src: `rule main = "a" $ "b"`, want: []string{"1:17 error never-matches"},
			msg: `begins with "b"`},
		{name: "end of line before a line break", src: `rule main = "a" $ "\n" "b"`},
		{name: "beginning of line after a character", src: `rule main = "a" ^ "b"`, want: []string{"1:17 error never-matches"}},
		{name: "beginning of line after a line feed", src: `rule main = "a\n" ^ "b"`},
		{name: "beginning of line after unknown text", src: "rule main = x ^ \"b\"\nrule x = \"a\" / \"\\n\""},
		{name: "recursion without a base case", src: `rule main = "(" main ")"`, want: []string{"1:1 error never-matches"},
			msg: "the recursion has no base case"},
		{name: "recursion with a base case", src: `rule main = "(" main ")" / "x"`},
		{name: "left recursion without a base case", src: `rule main = main "+" "x"`, want: []string{"1:1 error never-matches"}},
		{name: "mutual recursion", src: "rule main = \"(\" b \")\"\nrule b = \"[\" main \"]\"",
			want: []string{"1:1 error never-matches", "2:1 error never-matches"}},
		{name: "only the cycle is reported", src: "rule main = x\nrule x = \"(\" x \")\"", want: []string{"2:1 error never-matches"}},
		{name: "mutual recursion message", src: "rule main = \"(\" b \")\"\nrule b = \"[\" main \"]\"",
			want: []string{"1:1 error never-matches", "2:1 error never-matches"}, msg: "a match of one of main, b,"},
		// args calls itself, but only in an optional tail: it fails because expr does.
		{name: "recursion that is not the cause", src: "rule main = args\nrule args = expr (\",\" args)?\nrule expr = expr \"+\" \"t\"",
			want: []string{"2:23 hint right-recursion", "3:1 error never-matches"}},
		{name: "explicit failure", src: "rule main = x / \"y\"\nrule x = \"a\" _|_"},
		{name: "explicit failure in a recursion", src: `rule main = "(" main ")" / _|_ @error(message="m")`},
	})
}

func TestUselessLookahead(t *testing.T) {
	runCases(t, []lintCase{
		{name: "not of an optional", src: `rule main = !"a"? "b"`, want: []string{"1:13 error useless-lookahead"}, msg: "can never succeed"},
		{name: "and of a repetition", src: `rule main = &"a"* "b"`, want: []string{"1:13 warning useless-lookahead"}},
		{name: "not of bottom", src: `rule main = !_|_ "b"`, want: []string{"1:13 warning useless-lookahead"}},
		{name: "ordinary lookaheads", src: `rule main = !"a" &"b" . !.`},
		{name: "positive lookahead that captures", src: `rule main = &x:"a"? "b" => $x`},
	})
}

func TestNullableRepetition(t *testing.T) {
	runCases(t, []lintCase{
		{name: "optional element", src: `rule main = "a"?*`, want: []string{"1:13 warning nullable-repetition"}},
		{name: "nested repetition", src: `rule main = "a"*+`, want: []string{"1:13 warning nullable-repetition"}},
		{name: "lookahead element", src: `rule main = (&"a")+ "a"`, want: []string{"1:13 warning nullable-repetition"}},
		{name: "nullable rule", src: "rule main = ws*\nrule ws = [ ]*", want: []string{"1:13 warning nullable-repetition"}},
		{name: "consuming element", src: `rule main = ("a" "b"?)*`},
		// A line that is not at the end of the input must consume its line break: like csv.pego.
		{name: "not nullable because of the end of input", src: "rule main = line* $$\nrule line = !$$ [^\\n]* (\"\\n\" / $$)"},
		{name: "nullable at the end of input", src: "rule main = line* $$\nrule line = [^\\n]* (\"\\n\" / $$)",
			want: []string{"1:13 warning nullable-repetition"}},
		{name: "not nullable before the end of input", src: "rule main = line* $$\nrule line = \"x\"* !.",
			want: []string{"1:13 warning nullable-repetition"}},
	})
}

func TestRedundantOptional(t *testing.T) {
	runCases(t, []lintCase{
		{name: "optional repetition", src: `rule main = "a"*? "b"`, want: []string{"1:13 warning redundant-optional"}},
		{name: "double optional", src: `rule main = "a"??`, want: []string{"1:13 warning redundant-optional"}},
		{name: "optional", src: `rule main = "a"? "b"`},
		{name: "optional that a cut can make fail", src: `rule main = ("a" #cut "b"*)? "c"`},
	})
}

func TestUnusedCapture(t *testing.T) {
	runCases(t, []lintCase{
		{name: "not in the action", src: `rule main = a:"x" b:"y" => $a`, want: []string{"1:19 warning unused-capture"},
			msg: "capture b is never used"},
		{name: "no action", src: `rule main = a:"x" b:"y"`},
		{name: "read by a predicate", src: `rule main = a:"x" {len($a) > 0} b:"y" => $b`},
		{name: "optional group", src: `rule main = v:("," x:"a")? => $x`, want: []string{"1:13 warning unused-capture"}},
		{name: "discarded repetition", src: `rule main = (x:"a")* => nil`, want: []string{"1:14 warning unused-capture"},
			msg: "the value of the repetition it is in is discarded"},
		{name: "captured repetition", src: `rule main = r:(x:"a")* => $r`},
		{name: "repetition read with $n", src: `rule main = v1:(x:"a")* => $v1`},
		{name: "repetition element read by its predicate", src: `rule main = (x:"a" {len($x) > 0})* => nil`},
		{name: "lambda parameter of the same name", src: `rule main = r:(x:"a")* x:"b" => map($r, (x) => x.$x)`,
			want: []string{"1:24 warning unused-capture"}},
		{name: "terminal type", src: "type T terminal\nrule main T = a:\"x\"", want: []string{"2:15 warning unused-capture"},
			msg: "terminal of type T"},
		{name: "operatorsRule operator action", src: "rule main = operators {\n    operand [0-9]\n    level { infix left o:\"+\" => $lhs }\n}",
			want: []string{"3:24 warning unused-capture"}},
		{name: "operatorsRule operator without action", src: "rule main = operators {\n    operand [0-9]\n    level { infix left o:\"+\" }\n}"},
	})
}

func TestDuplicateCapture(t *testing.T) {
	runCases(t, []lintCase{
		{name: "sequence", src: `rule main = a:"x" a:"y"`, want: []string{"1:19 warning duplicate-capture"}},
		{name: "nested outer overwrites inner", src: `rule main = a:a:"x"`,
			want: []string{"1:13 warning duplicate-capture"}, msg: "overwriting the capture at 1:15"},
		{name: "choice", src: `rule main = a:"x" / a:"y"`},
		{name: "repetition", src: `rule main = (a:"x")* a:"y"`},
		{name: "after a choice", src: `rule main = (a:"x" / "z") a:"y"`, want: []string{"1:27 warning duplicate-capture"}},
	})
}

func TestCharClass(t *testing.T) {
	runCases(t, []lintCase{
		{name: "overlap", src: `rule main = [a-za-f]`, want: []string{"1:13 warning char-class"}, msg: "a-f overlaps a-z"},
		{name: "twice", src: `rule main = [_a_]`, want: []string{"1:13 warning char-class"}, msg: "_ is listed twice"},
		{name: "case span", src: `rule main = [A-z]`, want: []string{"1:13 warning char-class"}, msg: "A-z also matches \"[\\\\]^_`\""},
		{name: "digit span", src: `rule main = [0-Z]`, want: []string{"1:13 warning char-class"}},
		{name: "fine", src: `rule main = [a-zA-Z0-9_] [^"\\] [\--/]`},
	})
}

func TestUnreachableRule(t *testing.T) {
	runCases(t, []lintCase{
		{name: "unused", src: "rule main = \"a\"\nrule x = \"b\"\nrule y = x", want: []string{
			"2:1 warning unreachable-rule", "3:1 warning unreachable-rule"}},
		{name: "used in attributes and operatorsRule", src: "rule main = (e \";\") #recover(skip=s)\nrule s = [^;]+\n" +
			"rule e = operators {\n    operand n\n    level { infix left o }\n}\nrule n = [0-9]\nrule o = \"+\""},
	})
	got, _ := lintSource(t, "rule main = \"a\"\nrule x = \"b\"", Options{Disable: map[string]bool{CheckUnreachableRule: true}})
	if len(got) != 0 {
		t.Errorf("disabled: got %q", got)
	}
}

func TestRightRecursion(t *testing.T) {
	runCases(t, []lintCase{
		{name: "list", src: "rule main = line main?\nrule line = \"a\\n\"", want: []string{"1:18 hint right-recursion"}},
		{name: "separated list", src: "rule main = item (\",\" main)?\nrule item = \"a\"", want: []string{"1:23 hint right-recursion"}},
		{name: "prefix operator", src: `rule main = "-" main / "x"`},
		{name: "repetition", src: "rule main = line*\nrule line = \"a\\n\""},
		{name: "nested", src: "rule main = \"(\" main \")\" / \"x\""},
	})
}

func TestPositions(t *testing.T) {
	const at = "type At = { Pos int; Text Match }\n"
	runCases(t, []lintCase{
		{name: "repeated", src: at + "rule main = num*\nrule num At = t:[0-9]+ #atomic => At{Pos: $t.startPos, Text: $t}",
			want: []string{"3:46 hint positions"}, msg: "nor those of the 1 rule that calls it"},
		{name: "callers only", src: at + "rule main = num* other\nrule num At = t:[0-9]+ #atomic => At{Pos: $t.startPos, Text: $t}\n" +
			"rule other At = t:\"x\" => At{Pos: $t.endPos, Text: $t}",
			want: []string{"3:46 hint positions"}, msg: "nor those of the 1 rule that calls it"},
		{name: "once", src: at + "rule main = num\nrule num At = t:[0-9]+ #atomic => At{Pos: $t.startPos, Text: $t}"},
	})
}

func TestLongLookahead(t *testing.T) {
	runCases(t, []lintCase{
		{name: "to the end", src: `rule main = "a" &([^!]* "!") [^!]* "!"`, want: []string{"1:17 hint long-lookahead"}},
		{name: "any character", src: `rule main = !(.* "x") .*`, want: []string{"1:13 hint long-lookahead"}},
		{name: "a token", src: `rule main = !("if" ![a-z]) [a-z]+`},
		{name: "a line", src: `rule main = &([^\n]* "\n") [^\n]* "\n"`},
		{name: "through a rule", src: "rule main = &(ws \"x\") ws \"x\"\nrule ws = [^x]*"},
	})
}

func TestDirectives(t *testing.T) {
	runCases(t, []lintCase{
		{name: "before the rule", src: "// lint:ignore unused-capture the action needs only a\nrule main = (\n    a:\"x\"\n    b:\"y\"\n) => $a"},
		{name: "on the line", src: "rule main = a:\"x\" b:\"y\" => $a // lint:ignore unused-capture"},
		{name: "on the line before", src: "rule main = (\n    // lint:ignore unused-capture\n    a:\"x\" b:\"y\"\n) => $a"},
		{name: "two lines before", src: "rule main = (\n    // lint:ignore unused-capture\n    a:\"x\"\n    b:\"y\"\n) => $a",
			want: []string{"2:5 warning lint-directive", "4:5 warning unused-capture"}},
		{name: "another check", src: "rule main = a:\"x\" b:\"y\" => $a // lint:ignore char-class",
			want: []string{"1:19 warning unused-capture", "1:31 warning lint-directive"}, msg: "capture b"},
		{name: "several checks", src: "// lint:ignore char-class,unused-capture\nrule main = a:\"x\" b:\"y\" [aa] => $a"},
		{name: "file", src: "// lint:file-ignore unreachable-rule\npackage p\n\nrule main = \"a\"\n\nrule x = \"b\""},
		{name: "unknown check", src: "rule main = \"a\" // lint:ignore no-such-check",
			want: []string{"1:17 warning lint-directive"}, msg: "unknown checks: no-such-check"},
		{name: "no check", src: "rule main = \"a\" // lint:ignore", want: []string{"1:17 warning lint-directive"}},
		{name: "unused", src: "// lint:ignore unused-capture\nrule main = \"a\"", want: []string{"1:1 warning lint-directive"},
			msg: "suppresses no finding"},
		{name: "ordinary comment", src: "// lint is good\nrule main = \"a\""},
	})
	// A directive for a disabled check is not reported as unused.
	got, _ := lintSource(t, "// lint:ignore unused-capture\nrule main = \"a\"", Options{Disable: map[string]bool{CheckUnusedCapture: true}})
	if len(got) != 0 {
		t.Errorf("disabled: got %q", got)
	}
}

// TestExamples lints the example grammars. They have no findings: any finding is a false positive
// (or a new mistake in an example).
func TestExamples(t *testing.T) {
	files, err := filepath.Glob("../../examples/*/*.pego")
	if err != nil || len(files) == 0 {
		t.Fatalf("no example grammars: %v", err)
	}
	for _, f := range files {
		name, _ := filepath.Rel("../../examples", f)
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			got, fs := lintSource(t, string(src), Options{})
			if len(got) != 0 {
				for _, f := range fs {
					t.Log(f)
				}
				t.Errorf("got  %q\nwant none", got)
			}
		})
	}
}
