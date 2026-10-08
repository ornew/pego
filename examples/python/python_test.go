package python_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ornew/pego"
)

func compile(t testing.TB) *pego.Parser {
	t.Helper()
	src, err := os.ReadFile("python.pego")
	if err != nil {
		t.Fatal(err)
	}
	p, err := pego.CompileSource(string(src), "main")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Snippets that CPython accepts and that the grammar must parse without errors.
var valid = []string{
	"",
	"\n\n# only a comment\n",
	"pass",
	"x = 1  # no trailing newline",
	"x = 1; y = 2;\n",
	"a, b = b, a\n",
	"a = b = c = 0\n",
	"[a, *b], (c, d) = x, (1, 2)\n",
	"x.y[0].z = f()[1:2]\n",
	"x += 1\nx -= 1\nx *= 2\nx @= m\nx /= 2\nx //= 2\nx %= 3\nx **= 2\nx >>= 1\nx <<= 1\nx &= 1\nx |= 1\nx ^= 1\n",
	"x: int\ny: list[int] = []\nself.z: str = 'a'\n",
	"del a, b[0], c.d\n",
	"assert x, 'message'\n",
	"global a, b\n",
	"def f():\n    nonlocal x\n",
	"raise\nraise E\nraise E('x') from None\n",
	"return\n",
	"import a\nimport a.b.c as d, e\n",
	"from . import x\nfrom .. import (y as z,)\nfrom .m.n import *\nfrom m import a, b as c\n",
	"if a:\n    pass\nelif b:\n    pass\nelif c:\n    pass\nelse:\n    pass\n",
	"if a: x = 1; y = 2\nelse: z = 3\n",
	"while x:\n    break\nelse:\n    continue\n",
	"for i, (j, k) in enumerate(z):\n    pass\nelse:\n    pass\n",
	"for x in 1, 2, 3: print(x)\n",
	"try:\n    pass\nexcept:\n    pass\n",
	"try:\n    pass\nexcept (A, B) as e:\n    pass\nexcept C:\n    pass\nelse:\n    pass\nfinally:\n    pass\n",
	"try:\n    pass\nfinally:\n    pass\n",
	"with a: pass\n",
	"with a as b, c as (d, e), f: pass\n",
	"with (a as b, c as d,):\n    pass\n",
	"with (a, b) as c:\n    pass\n",
	"with (yield x) as y:\n    pass\n",
	"def f(a, b=1, /, c=2, *args, d, e=3, **kwargs): pass\n",
	"def f(*, a): pass\n",
	"def f(a: int, *b: str, c: 'x' = 1, **d: dict) -> list[int]:\n    return a\n",
	"def f(a, /): pass\n",
	"@a\n@b.c(1)\n@d[0]\n# comment between decorators\n\n@e\ndef f(): pass\n",
	"async def f():\n    await x\n    async for a in b: pass\n    async with c as d: pass\n    return [i async for i in aiter() if await i]\n",
	"@dec\nasync def f(): pass\n",
	"class A: pass\n",
	"class A(B, C, metaclass=M, **kw):\n    x = 1\n    def m(self): return self.x\n",
	"class A():\n    '''doc'''\n",
	"@dataclass(frozen=True)\nclass P:\n    x: int = 0\n",
	"x = lambda: 0\ny = lambda a, b=1, *c, d, **e: a\nz = lambda *, k: k\n",
	"x = a if b else c if d else e\n",
	"x = not a and b or not c\n",
	"x = a < b <= c > d >= e == f != g in h not in i is j is not k\n",
	"x = a | b ^ c & d << e >> f + g - h * i / j // k % l @ m\n",
	"x = -a ** -b ** ~c\n",
	"x = await a ** 2\n",
	"x = f(a)(b)[c].d\n",
	"x = f(*a, *b, k=1, **c, **d)\n",
	"x = f(a for a in b)\n",
	"x = f(a, k=1, *b)\n",
	"x = f(a,)\n",
	"x = [1, 2, 3,]\ny = [*a, *b]\nz = []\n",
	"x = (1,)\ny = ()\nz = (1)\nw = (*a, b)\n",
	"x = {}\ny = {1: 2, **d, 3: 4,}\nz = {1, 2, *s}\n",
	"x = [i for i in range(10) if i if i % 2 for j in range(i)]\n",
	"x = {k: v for k, v in d.items()}\ny = {i for i in s}\nz = (i for i in s)\n",
	"x = a[1:2, ::3, :, 4:, :5, ...]\n",
	"x = a[b:=1]\n",
	"if (n := len(a)) > 10: pass\n",
	"print(x := 1)\n",
	"def g():\n    x = yield\n    y = yield 1, 2\n    z = yield from w\n    yield\n",
	"x = 0xff + 0o17 + 0b1010 + 1_000_000 + 0x_FF\n",
	"x = 1.5 + .5 + 1. + 1e10 + 1.5E-3 + 1_0.0_1 + 3j + 1.5J + 1e3j\n",
	"x = 'a' \"b\" '''c''' \"\"\"d\"\"\"\n",
	"x = r'\\d' b'x' rb'y' BR'z' u'w' Rb'v'\n",
	"x = f'{a}' F\"{b!r:>{w}}\" rf'{c}' f'{{literal}}'\n",
	"x = f\"{d['key']}\" f'{x:{y}.{z}}'\n",
	"x = f\"{a[\"b\"]}\"\n",
	"x = 'it\\'s' \"say \\\"hi\\\"\"\n",
	"x = '''multi\nline\n'''\n",
	"x = ('implicit'\n     'concatenation')\n",
	"x = True, False, None, ...\n",
	"x = (1 +\n     2)\n",
	"x = [\n    1,  # comment\n\n    2,\n]\n",
	"x = {\n  'a': 1,\n  'b': [\n    2,\n  ],\n}\n",
	"f(a,\n  b,\n)\n",
	"x = 1 + \\\n    2\n",
	"if a and \\\n   b:\n    pass\n",
	"def f(\n    a,\n    b,\n):\n    pass\n",
	"class A:\n\n    def f(self):\n\n        pass\n\n\n    def g(self):\n        # comment\n            # oddly indented comment\n        pass\n",
	"if a:\n        deep = 1\n        if b:\n          deeper = 2\nshallow = 3\n",
	"if a:\n\tx = 1\n\tif b:\n\t\ty = 2\n",
	"x = (yield)\n",
	"print(*args, sep='')\n",
	"x = not_ = in_ = is_ = iffy = 1\n",
	"x = obj.if_ + obj.class_\n",
	"ünicode = 'ok'\n",
	"x = a.b.c.d(e).f[g](h)\n",
	"x = lambda x=lambda: 1: x\n",
	"x = [lambda: i for i in range(3)]\n",
	"t = a, *b\n",
	"for x, in y: pass\n",
	"x = -1 if a else +1\n",
	"x = a if b else lambda: c\n",
	"x = [x async for x in y]\n",
	"a = b if c else d, e\n",
	"def f(): return *a, b\n",
	"x = 1 if True else 2 if False else 3\n",
	"x = a[1][2](3)(4)\n",
	"x = (a)(b)\n",
	"x = {**a, 'b': 1}\n",
	"x = ~-+a\n",
	"x = not not a\n",
	"x = a ** b ** c\n",
	"x = 2 ** -1\n",
	"x = (a for b in c for d in e if f)\n",
	"x = a if b else(c)\n",
	"x = [i for i in(1, 2)]\n",
	"x = 1if a else 2\n",
}

// Snippets that CPython rejects and that the grammar must report as errors.
var invalid = []string{
	"x = \n",
	"x = (1, 2\n",
	"x = [1, 2\ny = 3\n",
	"f(a b)\n",
	"def f(:\n    pass\n",
	"def f()\n    pass\n",
	"class A(:\n    pass\n",
	"if x\n    pass\n",
	"if x:\npass\n",
	"if x:\n    a\n  b\n",
	"  x = 1\n",
	"x = 1\n    y = 2\n",
	"else:\n    pass\n",
	"try:\n    pass\n",
	"try:\n    pass\nelse:\n    pass\n",
	"for x y: pass\n",
	"while: pass\n",
	"x = 'unterminated\n",
	"x = '''unterminated\n",
	"import\n",
	"from import x\n",
	"from x import\n",
	"def = 1\n",
	"class = 1\n",
	"x = True False\n",
	"x = a if b\n",
	"x = lambda x: \n",
	"x = 1 +\n",
	"x = f(**)\n",
	"x = f(a=)\n",
	"x = f(a=1, b)\n",
	"x = [i for i in]\n",
	"x = {1: }\n",
	"x = a[]\n",
	"x = a.\n",
	"a = b += 1\n",
	"x += 1 = 2\n",
	"pass pass\n",
	"return return\n",
	"x = 1 2\n",
	"with a as : pass\n",
	"@dec\nx = 1\n",
	"async x = 1\n",
	"x = yield = 1\n" + "y = )\n",
}

func TestValid(t *testing.T) {
	p := compile(t)
	for _, src := range valid {
		if _, err := p.Parse(src); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
}

func TestInvalid(t *testing.T) {
	p := compile(t)
	for _, src := range invalid {
		if _, err := p.Parse(src); err == nil {
			t.Errorf("%q: no error", src)
		}
	}
}

// show renders an expression tree compactly to check the shape built by the precedence rules.
func show(v any) string {
	n, ok := v.(*pego.Node)
	if !ok || n == nil {
		return "nil"
	}
	f := func(name string) string { return show(n.Field(name)) }
	switch n.Type() {
	case "Name", "Constant", "Identifier":
		return n.Text
	case "BinOp", "BoolOp", "Compare":
		return "(" + f("Op") + " " + f("Left") + " " + f("Right") + ")"
	case "UnaryOp":
		return "(" + f("Op") + " " + f("Operand") + ")"
	case "Match":
		return strings.Join(strings.Fields(n.Text), " ")
	case "IfExp":
		return "(if " + f("Test") + " " + f("Body") + " " + f("OrElse") + ")"
	case "NamedExpr":
		return "(:= " + f("Target") + " " + f("Value") + ")"
	case "Await":
		return "(await " + f("Value") + ")"
	case "Call":
		return "(call " + f("Func") + " " + f("Args") + ")"
	case "Attribute":
		return "(. " + f("Value") + " " + f("Attr") + ")"
	case "Subscript":
		return "(sub " + f("Value") + " " + f("Slice") + ")"
	case "Tuple":
		return "(tuple " + f("Elts") + ")"
	case "Starred":
		return "(* " + f("Value") + ")"
	case "Lambda":
		return "(lambda " + f("Body") + ")"
	case "List":
		var parts []string
		for _, c := range n.Children {
			parts = append(parts, show(c))
		}
		return "[" + strings.Join(parts, " ") + "]"
	}
	return n.Type()
}

func TestPrecedence(t *testing.T) {
	p := compile(t)
	tests := []struct{ src, want string }{
		{"1 + 2 * 3", "(+ 1 (* 2 3))"},
		{"1 - 2 - 3", "(- (- 1 2) 3)"},
		{"2 ** 3 ** 4", "(** 2 (** 3 4))"},
		{"-2 ** 2", "(- (** 2 2))"},
		{"2 ** -1", "(** 2 (- 1))"},
		{"a * -b", "(* a (- b))"},
		{"await a ** b", "(** (await a) b)"},
		{"await a.b()", "(await (call (. a b) []))"},
		{"not a == b", "(not (== a b))"},
		{"not a and b", "(and (not a) b)"},
		{"a or b and c", "(or a (and b c))"},
		{"a and b or c", "(or (and a b) c)"},
		{"a < b < c", "(< (< a b) c)"},
		{"a not in b is not c", "(is not (not in a b) c)"},
		{"a | b ^ c & d", "(| a (^ b (& c d)))"},
		{"a << b + c", "(<< a (+ b c))"},
		{"a + b << c", "(<< (+ a b) c)"},
		{"a == b | c", "(== a (| b c))"},
		{"a @ b // c % d", "(% (// (@ a b) c) d)"},
		{"~a ** b", "(~ (** a b))"},
		{"a if b else c if d else e", "(if b a (if d c e))"},
		{"a or b if c or d else e", "(if (or c d) (or a b) e)"},
		{"lambda: a if b else c", "(lambda (if b a c))"},
		{"(x := a + 1)", "(:= x (+ a 1))"},
		{"a.b(c)[d]", "(sub (call (. a b) [c]) d)"},
		{"a, *b", "(tuple [a (* b)])"},
		{"-a.b", "(- (. a b))"},
	}
	for _, tt := range tests {
		n, err := p.Parse(tt.src + "\n")
		if err != nil {
			t.Errorf("%s: %v", tt.src, err)
			continue
		}
		body := n.Field("Body").(*pego.Node).Children
		got := show(body[0].Field("Value"))
		if got != tt.want {
			t.Errorf("%s:\n got  %s\n want %s", tt.src, got, tt.want)
		}
	}
}

// TestDeepNesting guards against exponential backtracking: no rule is memoized, so a rule
// that re-parsed its first element in a second alternative would double the work at each
// level of nesting.
func TestDeepNesting(t *testing.T) {
	p := compile(t)
	const d = 30
	nest := func(open, inner, close string) string {
		return "x = " + strings.Repeat(open, d) + inner + strings.Repeat(close, d) + "\n"
	}
	for _, src := range []string{
		nest("(", "1", ")"),
		nest("(1, ", "1", ")"),
		nest("[", "1", "]"),
		nest("{1: ", "1", "}"),
		nest("{", "1", "}"),
		nest("f(", "1", ")"),
		nest("f(k=", "1", ")"),
		nest("a[", "1", "]"),
		nest("a[1:", "1", "]"),
		nest("[x for x in ", "y", "]"),
		nest("a + b < (", "1", ")"),
		nest("a * b + c if (", "1", ") else d"),
		nest("a or not b == -c ** (", "1", ")"),
		nest("lambda: ", "1", ""),
		"if a:\n" + func() string {
			var b strings.Builder
			for i := 1; i <= d; i++ {
				b.WriteString(strings.Repeat(" ", i) + "if a:\n")
			}
			b.WriteString(strings.Repeat(" ", d+1) + "pass\n")
			return b.String()
		}(),
	} {
		if _, err := p.Parse(src); err != nil {
			t.Errorf("%.40q: %v", src, err)
		}
	}
}

// stdlibDir returns the directory of the Python standard library, or skips the test.
func stdlibDir(t *testing.T) (python, dir string) {
	t.Helper()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not found")
	}
	out, err := exec.Command(py, "-I", "-c", "import os; print(os.path.dirname(os.__file__))").Output()
	if err != nil {
		t.Skipf("python3: %v", err)
	}
	return py, strings.TrimSpace(string(out))
}

// Modules of the standard library that use only the supported subset (no match statements and
// no type parameter syntax). They exercise most statements and expressions at a realistic size.
var stdlibFiles = []string{
	"abc.py",
	"argparse.py",
	"ast.py",
	"base64.py",
	"bisect.py",
	"calendar.py",
	"collections/__init__.py",
	"contextlib.py",
	"copy.py",
	"csv.py",
	"datetime.py",
	"difflib.py",
	"enum.py",
	"fnmatch.py",
	"fractions.py",
	"functools.py",
	"gettext.py",
	"heapq.py",
	"inspect.py",
	"json/decoder.py",
	"json/encoder.py",
	"logging/__init__.py",
	"operator.py",
	"pathlib/_local.py",
	"pprint.py",
	"random.py",
	"re/_parser.py",
	"shlex.py",
	"string.py",
	"textwrap.py",
	"tokenize.py",
	"unittest/case.py",
	"weakref.py",
	"zipfile/__init__.py",
}

// countScript prints, for each file given as an argument, how many nodes of each kind
// CPython's ast module builds. It follows the representation of the grammar: comparisons
// and boolean operations count one node per operator (they are nested binary nodes in the
// grammar), and the contents of f-strings are not parsed.
const countScript = `
import ast, json, sys
names = {"List": "ListExpr", "keyword": "Keyword", "arg": "Arg", "alias": "Alias", "withitem": "WithItem",
         "comprehension": "Comprehension", "excepthandler": "ExceptHandler"}
result = {}
for path in sys.argv[1:]:
    try:
        with open(path, encoding="utf-8") as f:
            tree = ast.parse(f.read())
    except (SyntaxError, UnicodeDecodeError, ValueError):
        result[path] = None
        continue
    counts = {}
    def add(name, k=1):
        counts[name] = counts.get(name, 0) + k
    def visit(node):
        name = type(node).__name__
        if name == "Compare":
            add(name, len(node.ops))
        elif name == "BoolOp":
            add(name, len(node.values) - 1)
        elif name not in ("Load", "Store", "Del", "Module", "arguments") and not isinstance(
                node, (ast.operator, ast.unaryop, ast.cmpop, ast.boolop)):
            add(names.get(name, name))
        if name == "JoinedStr":
            return
        for child in ast.iter_child_nodes(node):
            visit(child)
    visit(tree)
    result[path] = counts
json.dump(result, sys.stdout)
`

// count counts the struct and terminal nodes of the AST by type, in the same way as countScript.
func count(n *pego.Node, counts map[string]int) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "List", "Seq", "Match", "Module", "Arguments", "Identifier", "DottedName", "DictItem", "DictUnpack":
	default:
		counts[n.Type()]++
	}
	for _, c := range n.Children {
		count(c, counts)
	}
	for _, f := range n.Fields {
		if c, ok := f.Value.(*pego.Node); ok {
			count(c, counts)
		}
	}
}

// TestStdlib parses modules of the installed Python standard library and checks that the
// AST has as many nodes of each kind as the one CPython builds.
func TestStdlib(t *testing.T) {
	py, dir := stdlibDir(t)
	var paths []string
	for _, f := range stdlibFiles {
		path := filepath.Join(dir, filepath.FromSlash(f))
		if _, err := os.Stat(path); err != nil {
			t.Logf("skip %s: %v", f, err)
			continue
		}
		paths = append(paths, path)
	}
	out, err := exec.Command(py, append([]string{"-I", "-c", countScript}, paths...)...).Output()
	if err != nil {
		t.Fatalf("python3: %v", err)
	}
	var want map[string]map[string]int
	if err := json.Unmarshal(out, &want); err != nil {
		t.Fatal(err)
	}
	p := compile(t)
	for _, path := range paths {
		rel, _ := filepath.Rel(dir, path)
		t.Run(filepath.ToSlash(rel), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			n, err := p.Parse(string(data))
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]int{}
			count(n, got)
			if diff := diffCounts(got, want[path]); diff != "" {
				t.Errorf("node counts differ from CPython (got, want):\n%s", diff)
			}
		})
	}
}

func diffCounts(got, want map[string]int) string {
	keys := map[string]bool{}
	for k := range got {
		keys[k] = true
	}
	for k := range want {
		keys[k] = true
	}
	var lines []string
	for k := range keys {
		if got[k] != want[k] {
			lines = append(lines, fmt.Sprintf("  %s: %d, %d", k, got[k], want[k]))
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// TestStdlibSurvey parses every module of the standard library outside the test packages and
// reports the files that fail or whose node counts differ from CPython's, and the slowest files.
// It is a development aid, enabled with PEGO_PYTHON_SURVEY=1 (or set to another directory of
// Python sources to survey instead of the standard library).
func TestStdlibSurvey(t *testing.T) {
	if os.Getenv("PEGO_PYTHON_SURVEY") == "" {
		t.Skip("set PEGO_PYTHON_SURVEY=1 to run")
	}
	py, dir := stdlibDir(t)
	if d := os.Getenv("PEGO_PYTHON_SURVEY"); d != "1" {
		dir = d
	}
	p := compile(t)
	var files []string
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		switch {
		case d.IsDir() && (d.Name() == "test" || d.Name() == "tests" || d.Name() == "idle_test" || d.Name() == "site-packages"):
			return filepath.SkipDir
		case strings.HasSuffix(path, ".py"):
			files = append(files, path)
		}
		return nil
	})
	out, err := exec.Command(py, append([]string{"-I", "-c", countScript}, files...)...).Output()
	if err != nil {
		t.Fatalf("python3: %v", err)
	}
	var want map[string]map[string]int
	if err := json.Unmarshal(out, &want); err != nil {
		t.Fatal(err)
	}
	type timing struct {
		name string
		d    time.Duration
	}
	var timings []timing
	ok := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(dir, f)
		if want[f] == nil {
			continue // CPython rejects it too
		}
		start := time.Now()
		n, err := p.Parse(string(data))
		timings = append(timings, timing{rel, time.Since(start)})
		if err != nil {
			msg, _, _ := strings.Cut(err.Error(), "\n")
			t.Logf("FAIL %s: %.160s", rel, msg)
			continue
		}
		got := map[string]int{}
		count(n, got)
		if diff := diffCounts(got, want[f]); diff != "" {
			t.Logf("DIFF %s (got, want):\n%s", rel, diff)
			continue
		}
		ok++
	}
	sort.Slice(timings, func(i, j int) bool { return timings[i].d > timings[j].d })
	for _, s := range timings[:min(5, len(timings))] {
		t.Logf("slow %s %v", s.name, s.d)
	}
	t.Logf("%d/%d files parsed with the same node counts as CPython", ok, len(files))
}
