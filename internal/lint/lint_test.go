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
		{name: "literal prefix", src: `def main = "a" / "ab"`,
			want: []string{"1:18 error shadowed-alternative"}, msg: "alternative 2 (`\"ab\"`) can never match: alternative 1"},
		{name: "longer literal first", src: `def main = "ab" / "a"`},
		{name: "operator prefix", src: `def main = "-" / "->" / "+"`,
			want: []string{"1:18 error shadowed-alternative"}},
		{name: "literal through a rule", src: "def main = op / \"==\"\ndef op = \"=\"",
			want: []string{"1:17 error shadowed-alternative"}},
		{name: "keyword after identifier", src: "def main = ident / \"if\"\ndef ident = @(?a-z)+",
			want: []string{"1:20 error shadowed-alternative"}},
		{name: "keyword after identifier with a boundary", src: "def main = ident / \"if\" !(?a-z0-9)\ndef ident = @(?a-z)+",
			want: []string{"1:20 error shadowed-alternative"}},
		{name: "keyword before identifier", src: "def main = \"if\" !(?a-z) / ident\ndef ident = @(?a-z)+"},
		{name: "keyword without boundary before identifier", src: "def main = kw / ident\ndef kw = \"if\" !(?a-z)\ndef ident = @(?a-z)+"},
		{name: "same items first", src: "def main = x / x \"(\" \")\"\ndef x = \"x\"",
			want: []string{"1:16 error shadowed-alternative"}, msg: "matches first wherever it could"},
		{name: "same items with an optional tail", src: "def main = x y? / x \"(\"\ndef x = \"x\"\ndef y = \"y\"",
			want: []string{"1:19 error shadowed-alternative"}},
		{name: "same items, then a longer literal", src: "def main = x \".\" / x \"..\"\ndef x = \"x\"",
			want: []string{"1:20 error shadowed-alternative"}},
		{name: "longer alternative first", src: "def main = x \"(\" \")\" / x\ndef x = \"x\""},
		{name: "duplicate", src: `def main = "a" / "b" / "a"`,
			want: []string{"1:24 error shadowed-alternative"}, msg: "it is the same as alternative 1"},
		{name: "capture names do not matter", src: `def main = a:"x" "y" / b:"x" "y"`,
			want: []string{"1:24 error shadowed-alternative"}},
		{name: "capture names matter to predicates", src: `def main = a:"x" [len($a) > 1] / b:"x" [len($b) > 2]`},
		{name: "predicates are not certain", src: `def main = a:"x" [len($a) > 1] / "x"`},
		{name: "always succeeds", src: `def main = "a"? / "b" / "c"`,
			want: []string{"1:19 error shadowed-alternative"}, msg: "alternatives 2 to 3 are never tried: alternative 1"},
		{name: "always succeeds, one after", src: `def main = "a"* / "b"`,
			want: []string{"1:19 error shadowed-alternative"}, msg: "alternative 2 (`\"b\"`) is never tried"},
		{name: "always succeeds at the end", src: `def main = "a" / _`},
		// While r grows its left recursion, the call of r returns what it has found so far: at
		// first, a failure. "a" is tried then.
		{name: "left-recursive call", src: "def main = r\ndef r = r / \"a\""},
		{name: "left-recursive call, then something else", src: "def main = r\ndef r = r \"x\" / \"a\" / \"a\"",
			want: []string{"2:23 error shadowed-alternative"}},
		{name: "a cut can make an optional fail", src: `def main = ("a" -- "b")? / "c"`},
		{name: "a cut inside a choice stays there", src: `def main = ("a" -- "b" / "x")? / "c"`,
			want: []string{"1:34 error shadowed-alternative"}},
		{name: "a cut can make the choice fail first", src: `def main = ("a" -- "b" / "a"? "c") / "d"`},
		{name: "recovery is not a prefix", src: `def main = ("x" "y") #recover(skip=(?^;)+) / "z"`},
		{name: "explicit failure with a message", src: `def main = "hello" / _|_ #error(message="expected hello")`},
		{name: "nested choice", src: `def main = "(" ("<" / "<=") ")"`,
			want: []string{"1:23 error shadowed-alternative"}},
		{name: "pratt operands", src: "def main = pratt {\n operand \"a\"\n operand \"ab\"\n level { infix left \"+\" }\n}",
			want: []string{"3:10 error shadowed-alternative"}, msg: "operand 2"},
		{name: "pratt duplicate operator", src: "def main = pratt {\n operand (?0-9)\n level { infix left \"+\" infix left \"+\" }\n}",
			want: []string{"3:25 error shadowed-alternative"}, msg: "is never selected"},
		{name: "pratt operators of different kinds", src: "def main = pratt {\n operand (?0-9)\n level { prefix \"-\" infix left \"-\" }\n}"},
		{name: "pratt longest match", src: "def main = pratt {\n operand (?0-9)\n level { infix left \"<\" infix left \"<=\" }\n}"},
		{name: "pratt operators on levels a call can leave out",
			src: "def main = e(b) \"!\"\ndef e = pratt {\n operand (?0-9)\n level a { infix left \"+\" }\n level b { infix left \"+\" }\n}"},
		{name: "pratt operators on levels no call leaves out",
			src:  "def main = e \"!\"\ndef e = pratt {\n operand (?0-9)\n level a { infix left \"+\" }\n level b { infix left \"+\" }\n}",
			want: []string{"5:12 error shadowed-alternative"}},
	})
}

func TestNeverMatches(t *testing.T) {
	runCases(t, []lintCase{
		{name: "end of input before input", src: `def main = "a" $$ "b"`, want: []string{"1:16 error never-matches"}},
		{name: "end of input before nullable", src: `def main = "a" $$ "b"*`},
		{name: "end of input before a lookahead for input", src: `def main = "a" $$ &.`, want: []string{"1:16 error never-matches"}},
		{name: "end of input before a lookahead for the end", src: `def main = "a" $$ !.`},
		{name: "beginning of input after input", src: `def main = "a" ^^ "b"`, want: []string{"1:16 error never-matches"}},
		{name: "beginning of input after nullable", src: `def main = "a"? ^^ "b"`},
		{name: "end of line before a character", src: `def main = "a" $ "b"`, want: []string{"1:16 error never-matches"},
			msg: `begins with "b"`},
		{name: "end of line before a line break", src: `def main = "a" $ "\n" "b"`},
		{name: "beginning of line after a character", src: `def main = "a" ^ "b"`, want: []string{"1:16 error never-matches"}},
		{name: "beginning of line after a line feed", src: `def main = "a\n" ^ "b"`},
		{name: "beginning of line after unknown text", src: "def main = x ^ \"b\"\ndef x = \"a\" / \"\\n\""},
		{name: "recursion without a base case", src: `def main = "(" main ")"`, want: []string{"1:1 error never-matches"},
			msg: "the recursion has no base case"},
		{name: "recursion with a base case", src: `def main = "(" main ")" / "x"`},
		{name: "left recursion without a base case", src: `def main = main "+" "x"`, want: []string{"1:1 error never-matches"}},
		{name: "mutual recursion", src: "def main = \"(\" b \")\"\ndef b = \"[\" main \"]\"",
			want: []string{"1:1 error never-matches", "2:1 error never-matches"}},
		{name: "only the cycle is reported", src: "def main = x\ndef x = \"(\" x \")\"", want: []string{"2:1 error never-matches"}},
		{name: "mutual recursion message", src: "def main = \"(\" b \")\"\ndef b = \"[\" main \"]\"",
			want: []string{"1:1 error never-matches", "2:1 error never-matches"}, msg: "a match of one of main, b,"},
		// args calls itself, but only in an optional tail: it fails because expr does.
		{name: "recursion that is not the cause", src: "def main = args\ndef args = expr (\",\" args)?\ndef expr = expr \"+\" \"t\"",
			want: []string{"2:22 hint right-recursion", "3:1 error never-matches"}},
		{name: "explicit failure", src: "def main = x / \"y\"\ndef x = \"a\" _|_"},
		{name: "explicit failure in a recursion", src: `def main = "(" main ")" / _|_ #error(message="m")`},
	})
}

func TestUselessLookahead(t *testing.T) {
	runCases(t, []lintCase{
		{name: "not of an optional", src: `def main = !"a"? "b"`, want: []string{"1:12 error useless-lookahead"}, msg: "can never succeed"},
		{name: "and of a repetition", src: `def main = &"a"* "b"`, want: []string{"1:12 warning useless-lookahead"}},
		{name: "not of bottom", src: `def main = !_|_ "b"`, want: []string{"1:12 warning useless-lookahead"}},
		{name: "ordinary lookaheads", src: `def main = !"a" &"b" . !.`},
		{name: "positive lookahead that captures", src: `def main = &(x:"a"?) "b" -> $x`},
	})
}

func TestNullableRepetition(t *testing.T) {
	runCases(t, []lintCase{
		{name: "optional element", src: `def main = ("a"?)*`, want: []string{"1:12 warning nullable-repetition"}},
		{name: "nested repetition", src: `def main = ("a"*)+`, want: []string{"1:12 warning nullable-repetition"}},
		{name: "lookahead element", src: `def main = (&"a")+ "a"`, want: []string{"1:12 warning nullable-repetition"}},
		{name: "nullable rule", src: "def main = ws*\ndef ws = (? )*", want: []string{"1:12 warning nullable-repetition"}},
		{name: "consuming element", src: `def main = ("a" "b"?)*`},
		// A line that is not at the end of the input must consume its line break: like csv.pego.
		{name: "not nullable because of the end of input", src: "def main = line* $$\ndef line = !$$ (?^\\n)* (\"\\n\" / $$)"},
		{name: "nullable at the end of input", src: "def main = line* $$\ndef line = (?^\\n)* (\"\\n\" / $$)",
			want: []string{"1:12 warning nullable-repetition"}},
		{name: "not nullable before the end of input", src: "def main = line* $$\ndef line = \"x\"* !.",
			want: []string{"1:12 warning nullable-repetition"}},
	})
}

func TestRedundantOptional(t *testing.T) {
	runCases(t, []lintCase{
		{name: "optional repetition", src: `def main = ("a"*)? "b"`, want: []string{"1:12 warning redundant-optional"}},
		{name: "double optional", src: `def main = "a"??`, want: []string{"1:12 warning redundant-optional"}},
		{name: "optional", src: `def main = "a"? "b"`},
		{name: "optional that a cut can make fail", src: `def main = ("a" -- "b"*)? "c"`},
	})
}

func TestUnusedCapture(t *testing.T) {
	runCases(t, []lintCase{
		{name: "not in the action", src: `def main = a:"x" b:"y" -> $a`, want: []string{"1:18 warning unused-capture"},
			msg: "capture b is never used"},
		{name: "no action", src: `def main = a:"x" b:"y"`},
		{name: "read by a predicate", src: `def main = a:"x" [len($a) > 0] b:"y" -> $b`},
		{name: "optional group", src: `def main = v:("," x:"a")? -> $x`, want: []string{"1:12 warning unused-capture"}},
		{name: "discarded repetition", src: `def main = (x:"a")* -> nil`, want: []string{"1:13 warning unused-capture"},
			msg: "the value of the repetition it is in is discarded"},
		{name: "captured repetition", src: `def main = r:(x:"a")* -> $r`},
		{name: "repetition read with $n", src: `def main = (x:"a")* -> $1`},
		{name: "repetition element read by its predicate", src: `def main = (x:"a" [len($x) > 0])* -> nil`},
		{name: "lambda parameter of the same name", src: `def main = r:(x:"a")* x:"b" -> map($r, (x) => $x.x)`,
			want: []string{"1:23 warning unused-capture"}},
		{name: "terminal type", src: "type T terminal\ndef main: T = a:\"x\"", want: []string{"2:15 warning unused-capture"},
			msg: "terminal of type T"},
		{name: "pratt operator action", src: "def main = pratt {\n operand (?0-9)\n level { infix left o:\"+\" -> $lhs }\n}",
			want: []string{"3:21 warning unused-capture"}},
		{name: "pratt operator without action", src: "def main = pratt {\n operand (?0-9)\n level { infix left o:\"+\" }\n}"},
	})
}

func TestDuplicateCapture(t *testing.T) {
	runCases(t, []lintCase{
		{name: "sequence", src: `def main = a:"x" a:"y"`, want: []string{"1:18 warning duplicate-capture"}},
		{name: "nested", src: `def main = a:(a:"x")`, want: []string{"1:15 warning duplicate-capture"}},
		{name: "choice", src: `def main = a:"x" / a:"y"`},
		{name: "repetition", src: `def main = (a:"x")* a:"y"`},
		{name: "after a choice", src: `def main = (a:"x" / "z") a:"y"`, want: []string{"1:26 warning duplicate-capture"}},
	})
}

func TestCharClass(t *testing.T) {
	runCases(t, []lintCase{
		{name: "overlap", src: `def main = (?a-za-f)`, want: []string{"1:12 warning char-class"}, msg: "a-f overlaps a-z"},
		{name: "twice", src: `def main = (?_a_)`, want: []string{"1:12 warning char-class"}, msg: "_ is listed twice"},
		{name: "case span", src: `def main = (?A-z)`, want: []string{"1:12 warning char-class"}, msg: "A-z also matches \"[\\\\]^_`\""},
		{name: "digit span", src: `def main = (?0-Z)`, want: []string{"1:12 warning char-class"}},
		{name: "fine", src: `def main = (?a-zA-Z0-9_) (?^"\\) (?--/)`},
	})
}

func TestUnreachableRule(t *testing.T) {
	runCases(t, []lintCase{
		{name: "unused", src: "def main = \"a\"\ndef x = \"b\"\ndef y = x", want: []string{
			"2:1 warning unreachable-rule", "3:1 warning unreachable-rule"}},
		{name: "used in attributes and pratt", src: "def main = (e \";\") #recover(skip=s)\ndef s = (?^;)+\n" +
			"def e = pratt {\n operand n\n level { infix left o }\n}\ndef n = (?0-9)\ndef o = \"+\""},
	})
	got, _ := lintSource(t, "def main = \"a\"\ndef x = \"b\"", Options{Disable: map[string]bool{CheckUnreachableRule: true}})
	if len(got) != 0 {
		t.Errorf("disabled: got %q", got)
	}
}

func TestRightRecursion(t *testing.T) {
	runCases(t, []lintCase{
		{name: "list", src: "def main = line main?\ndef line = \"a\\n\"", want: []string{"1:17 hint right-recursion"}},
		{name: "separated list", src: "def main = item (\",\" main)?\ndef item = \"a\"", want: []string{"1:22 hint right-recursion"}},
		{name: "prefix operator", src: `def main = "-" main / "x"`},
		{name: "repetition", src: "def main = line*\ndef line = \"a\\n\""},
		{name: "nested", src: "def main = \"(\" main \")\" / \"x\""},
	})
}

func TestPositions(t *testing.T) {
	const at = "type At struct { Pos int, Text Match }\n"
	runCases(t, []lintCase{
		{name: "repeated", src: at + "def main = num*\ndef num: At = t:@(?0-9)+ -> new At{Pos: $t.startPos, Text: $t}",
			want: []string{"3:44 hint positions"}, msg: "nor those of the 1 rule that calls it"},
		{name: "callers only", src: at + "def main = num* other\ndef num: At = t:@(?0-9)+ -> new At{Pos: $t.startPos, Text: $t}\n" +
			"def other: At = t:\"x\" -> new At{Pos: $t.endPos, Text: $t}",
			want: []string{"3:44 hint positions"}, msg: "nor those of the 1 rule that calls it"},
		{name: "once", src: at + "def main = num\ndef num: At = t:@(?0-9)+ -> new At{Pos: $t.startPos, Text: $t}"},
	})
}

func TestLongLookahead(t *testing.T) {
	runCases(t, []lintCase{
		{name: "to the end", src: `def main = "a" &((?^!)* "!") (?^!)* "!"`, want: []string{"1:16 hint long-lookahead"}},
		{name: "any character", src: `def main = !(.* "x") .*`, want: []string{"1:12 hint long-lookahead"}},
		{name: "a token", src: `def main = !("if" !(?a-z)) (?a-z)+`},
		{name: "a line", src: `def main = &((?^\n)* "\n") (?^\n)* "\n"`},
		{name: "through a rule", src: "def main = &(ws \"x\") ws \"x\"\ndef ws = (?^x)*"},
	})
}

func TestDirectives(t *testing.T) {
	runCases(t, []lintCase{
		{name: "before the rule", src: "// lint:ignore unused-capture the action needs only a\ndef main =\n  a:\"x\"\n  b:\"y\" -> $a"},
		{name: "on the line", src: "def main = a:\"x\" b:\"y\" -> $a // lint:ignore unused-capture"},
		{name: "on the line before", src: "def main =\n  // lint:ignore unused-capture\n  a:\"x\" b:\"y\"\n  -> $a"},
		{name: "two lines before", src: "def main =\n  // lint:ignore unused-capture\n  a:\"x\"\n  b:\"y\" -> $a",
			want: []string{"2:3 warning lint-directive", "4:3 warning unused-capture"}},
		{name: "another check", src: "def main = a:\"x\" b:\"y\" -> $a // lint:ignore char-class",
			want: []string{"1:18 warning unused-capture", "1:30 warning lint-directive"}, msg: "capture b"},
		{name: "several checks", src: "// lint:ignore char-class,unused-capture\ndef main = a:\"x\" b:\"y\" (?aa) -> $a"},
		{name: "file", src: "// lint:file-ignore unreachable-rule\npackage p\n\ndef main = \"a\"\n\ndef x = \"b\""},
		{name: "unknown check", src: "def main = \"a\" // lint:ignore no-such-check",
			want: []string{"1:16 warning lint-directive"}, msg: "unknown checks: no-such-check"},
		{name: "no check", src: "def main = \"a\" // lint:ignore", want: []string{"1:16 warning lint-directive"}},
		{name: "unused", src: "// lint:ignore unused-capture\ndef main = \"a\"", want: []string{"1:1 warning lint-directive"},
			msg: "suppresses no finding"},
		{name: "ordinary comment", src: "// lint is good\ndef main = \"a\""},
	})
	// A directive for a disabled check is not reported as unused.
	got, _ := lintSource(t, "// lint:ignore unused-capture\ndef main = \"a\"", Options{Disable: map[string]bool{CheckUnusedCapture: true}})
	if len(got) != 0 {
		t.Errorf("disabled: got %q", got)
	}
}

// TestExamples lints the example grammars. Every finding listed here has been checked by hand;
// anything else is a false positive (or a new mistake in an example).
func TestExamples(t *testing.T) {
	want := map[string][]string{
		// Captures of optional groups whose own captures are what the actions read.
		"python/python.pego": {
			"197:32 warning unused-capture", "198:29 warning unused-capture", "198:48 warning unused-capture",
			"205:48 warning unused-capture", "209:41 warning unused-capture", "220:40 warning unused-capture",
			"265:55 warning unused-capture", "265:72 warning unused-capture", "275:40 warning unused-capture",
			"306:5 warning unused-capture", "306:9 warning unused-capture", "307:5 warning unused-capture",
			"311:45 warning unused-capture", "346:29 warning unused-capture",
		},
	}
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
			if !slices.Equal(got, want[filepath.ToSlash(name)]) {
				for _, f := range fs {
					t.Log(f)
				}
				t.Errorf("got  %q\nwant %q", got, want[filepath.ToSlash(name)])
			}
		})
	}
}
