package typescript_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/typescript"
)

// TestGolden checks the generated parser against the golden files of testdata, which the tests of
// github.com/ornew/pego/parsers check every backend of the engine against. An input starting with NUL is
// parsed as a .tsx file (see ParseTSX).
func TestGolden(t *testing.T) {
	inputs, _ := filepath.Glob("testdata/*.txt")
	if len(inputs) == 0 {
		t.Fatal("no inputs")
	}
	for _, in := range inputs {
		data, err := os.ReadFile(in)
		if err != nil {
			t.Fatal(err)
		}
		var got string
		n, err := typescript.Parse(string(data))
		if n != nil {
			got = n.String()
		}
		if err != nil {
			if got != "" {
				got += "\n"
			}
			got += "error: " + err.Error()
		}
		want, err := os.ReadFile(strings.TrimSuffix(in, ".txt") + ".golden")
		if err != nil {
			t.Fatal(err)
		}
		if got+"\n" != string(want) {
			t.Errorf("%s\n got  %s\n want %s", in, got, want)
		}
	}
}

// valid and invalid are sources that Parse and Recognize must accept and reject, as the TypeScript compiler's
// parser does (the differential tests, which need node, check the same on the compiler's own test cases).
var valid = []string{
	"",
	"#!/usr/bin/env node\nlet a = 1",
	"let x: number = 1, y = 'two', z = `three ${x + y}`;",
	"var a = 1\nvar b = 2\n[a, b] = [b, a]",
	"export default class extends Base<T> implements I, J { static #n = 0; get v(): number { return 1 } }",
	"abstract class A<in out T extends object = {}> { abstract m(): void; protected constructor(private readonly x: T) { super() } }",
	"declare module 'm' { export function f<T>(x: T): T; export = f }",
	"namespace A.B { export const c = 1 } enum E { A = 1, B = A << 1, C = 'c'.length }",
	"type T<K extends string> = { readonly [P in K as `get${Capitalize<P>}`]?: () => P } | [a: 1, b?: 2, ...rest: 3[]]",
	"type C<T> = T extends (infer U)[] ? U : T extends readonly unknown[] ? never : T",
	"function f(this: Window, a?: number, ...b: string[]): asserts a is number {}\nfunction f(x: string): void\nfunction f() {}",
	"const f = async <T,>(x: T): Promise<T> => { for await (const y of x) { await y } return x }",
	"let o = { a, b: 1, [c]: 2, get d() { return 1 }, async *e() {}, ...f, 'g': 3, 4: 5 }",
	"x = a ? b : c ? d : e; y = a ?? b || c; z ||= 1; w = a?.b?.[c]?.(d) ?? e!",
	"for (const [k, v] of Object.entries(o)) {} for (let i = 0, n = 1; i < n; i++) ; for (x in y) ; while (1) break; do ; while (0)",
	"label: { switch (a) { case 1: case 2: break label; default: throw new Error() } }",
	"try { f() } catch { } finally { }\ntry { f() } catch (e: unknown) { }",
	"import a, { b as c, type d, 'e' as f } from 'm' with { type: 'json' }\nexport { c as default, type d }; export * as ns from 'n'; export type { T } from 't'",
	"import x = require('x'); import type y = x.y; export import z = x.z",
	"using a = f(); await using b = g();",
	"@dec class A { @a @b m(@p x: number) {} @c accessor y = 1; static { init() } }",
	"let v = a satisfies B as C; let w = <D>e; let u = new.target; let t = import.meta.url; let s = import('m')",
	"let r = /ab+c/gi.test(s) / 2; let q = a / b / c; let p = a++ / b",
	"let n = 0x1f + 0b101 + 0o17 + 1_000 + .5e-3 + 10n + 0xFFn",
	"class C { 'constructor'() {} ['x']() {} 1() {} static async *[Symbol.iterator]() {} declare readonly y: number; z!: string }",
	"if (a) b; else if (c) d; else { e }",
	"f<string>(x); a < b > c; new Map<string, Array<number>>(); g<T>`tpl`; h?.<T>()",
	"let a = function* () { yield; yield* b; const c = yield d }; let e = class { }",
	"let { a, b: { c }, ...d } = e; let [f, , g = 1, ...h] = i",
	"declare global { interface Window { x: number } }\ndeclare const enum E { A }\ndeclare function f(): void",
	"let x = y as const; let z = <const>['a']; type K = keyof typeof o; type U = unique symbol; type Q = typeof import('m')",
	"abstract class B { private static readonly x?: number; public override m(): this { return this } }",
	"const t = (a: number, b?: string) => a; const u = (a) => a; const v = a => a; const w = async a => a; const x = async (a) => a",
}

var validTSX = []string{
	"let a = <div className=\"a\" {...p} key={k}>text {x} <b>bold</b> <></> <A.B c:d='e' /></div>",
	"let f = <T,>(x: T) => x; let g = <T extends unknown>(x: T) => x;",
	"let a = <div>{items.map((i) => <li key={i}>{i}</li>)}</div>",
	"let a = <A<string> prop=\"x\" />",
}

var invalid = []string{
	"let x = ;",
	"function () {}",
	"a = 1 +",
	"if (a",
	"{ a } = b",
	"let [a = 1",
	"x ? y",
	"for (;;",
	"class A { m(: void }",
	"type T = ;",
	"interface I { x: }",
	"enum { A }",
	"import { from 'm'",
	"a b",
	"1a",
	"'unterminated",
	"`unterminated ${",
	"/* unterminated",
	"let x = 08;",
	"let a = <div></span>",
	"x = {a b}",
}

func TestValid(t *testing.T) {
	for _, src := range valid {
		if _, err := typescript.ParseAST(src); err != nil {
			t.Errorf("%q: %v", src, err)
		}
		if err := typescript.Recognize(src); err != nil {
			t.Errorf("Recognize %q: %v", src, err)
		}
	}
	for _, src := range validTSX {
		if _, err := typescript.ParseTSX(src); err != nil {
			t.Errorf("tsx %q: %v", src, err)
		}
		if err := typescript.RecognizeTSX(src); err != nil {
			t.Errorf("RecognizeTSX %q: %v", src, err)
		}
	}
}

func TestInvalid(t *testing.T) {
	for _, src := range invalid {
		if _, err := typescript.ParseAST(src); err == nil {
			t.Errorf("%q: accepted", src)
		}
		if err := typescript.Recognize(src); err == nil {
			t.Errorf("Recognize %q: accepted", src)
		}
	}
	// A type assertion is not valid in a .tsx file, and JSX is not valid in a .ts file.
	if _, err := typescript.ParseTSX("let a = <T>b;"); err == nil {
		t.Error("tsx: a type assertion was accepted")
	}
	if _, err := typescript.ParseAST("let a = <div>b</div>;"); err == nil {
		t.Error("ts: JSX was accepted")
	}
}

// TestSpans checks that the range of every node holds those of its children, which follow each other in the
// order of the input.
func TestSpans(t *testing.T) {
	for _, src := range append(append([]string{}, valid...), "x = <div a={1}>{b}</div>") {
		parse := typescript.ParseAST
		if strings.HasPrefix(src, "x = <div") {
			parse = func(s string, u ...typescript.Unit) (*typescript.SourceFile, error) {
				return typescript.ParseTSX(s, u...)
			}
		}
		f, err := parse(src)
		if err != nil {
			t.Fatal(err)
		}
		var check func(n typescript.ASTNode)
		check = func(n typescript.ASTNode) {
			s, e := n.Range()
			if s < 0 || e < s || e > len([]rune(src)) {
				t.Errorf("%q: %s has range %d-%d", src, typescript.Kind(n), s, e)
			}
			prev := s
			typescript.ForEachChild(n, func(c typescript.ASTNode) bool {
				cs, ce := c.Range()
				if cs < prev || ce > e {
					t.Errorf("%q: %s %d-%d has the child %s %d-%d after %d", src, typescript.Kind(n), s, e, typescript.Kind(c), cs, ce, prev)
				}
				prev = ce
				check(c)
				return true
			})
		}
		check(f)
	}
}

func TestUnits(t *testing.T) {
	src := "let ü = '日本語'; let b = 1"
	cp, err := typescript.ParseAST(src)
	if err != nil {
		t.Fatal(err)
	}
	by, err := typescript.ParseAST(src, typescript.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	cs, _ := cp.Statements[1].(*typescript.VariableStatement).Range()
	bs, _ := by.Statements[1].(*typescript.VariableStatement).Range()
	if cs != 15 || bs != 22 {
		t.Errorf("second statement starts at %d code points, %d bytes; want 15, 22", cs, bs)
	}
}

func TestParseFile(t *testing.T) {
	// await at the top level of a module is an await expression; in a script it is an identifier.
	for _, c := range []struct {
		name, src string
		module    bool
		await     bool
	}{
		{"a.ts", "export {}; const x = await (y);", true, true},
		{"a.ts", "const x = await (y);", false, false},
		{"a.d.ts", "export {}; const x = await (y);", true, false},
		{"a.tsx", "import 'm'; const x = await (<div/>);", true, true},
	} {
		f, err := typescript.ParseFile(c.name, c.src)
		if err != nil {
			t.Errorf("%s %q: %v", c.name, c.src, err)
			continue
		}
		if got := typescript.IsExternalModule(f); got != c.module {
			t.Errorf("%s %q: IsExternalModule = %v, want %v", c.name, c.src, got, c.module)
		}
		init := f.Statements[len(f.Statements)-1].(*typescript.VariableStatement).DeclarationList.Declarations[0].Initializer
		if _, ok := init.(*typescript.AwaitExpression); ok != c.await {
			t.Errorf("%s %q: await expression = %v, want %v", c.name, c.src, ok, c.await)
		}
	}
	if !typescript.IsTSX("A.TSX") || !typescript.IsTSX("a.jsx") || typescript.IsTSX("a.ts") {
		t.Error("IsTSX")
	}
}

func TestErrors(t *testing.T) {
	_, err := typescript.ParseTSX("\nlet a = ;")
	var se *typescript.SyntaxError
	if !errors.As(err, &se) || se.Line != 2 || se.Col != 9 || se.Pos != 9 {
		t.Errorf("ParseTSX error = %#v, want line 2, column 9, position 9", err)
	}
	_, err = typescript.ParseTSX("let a = ;")
	if !errors.As(err, &se) || se.Line != 1 || se.Col != 9 || se.Pos != 8 {
		t.Errorf("ParseTSX error = %#v, want line 1, column 9, position 8", err)
	}
	if err := typescript.RecognizeTSX("let a = ;"); err == nil || err.Error() != se.Error() {
		t.Errorf("RecognizeTSX error = %v, want %v", err, se)
	}
}

func TestKind(t *testing.T) {
	f, err := typescript.ParseAST("a + b && c; this; null; true; x as string; `t`; super.y")
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	typescript.Inspect(f, func(n typescript.ASTNode) bool {
		if n != nil {
			kinds = append(kinds, typescript.Kind(n))
		}
		return true
	})
	want := "SourceFile ExpressionStatement BinaryExpression BinaryExpression Identifier PlusToken Identifier AmpersandAmpersandToken Identifier " +
		"ExpressionStatement ThisKeyword ExpressionStatement NullKeyword ExpressionStatement TrueKeyword ExpressionStatement AsExpression " +
		"Identifier StringKeyword ExpressionStatement NoSubstitutionTemplateLiteral ExpressionStatement PropertyAccessExpression SuperKeyword Identifier EndOfFileToken"
	if got := strings.Join(kinds, " "); got != want {
		t.Errorf("kinds\n got  %s\n want %s", got, want)
	}
	if typescript.Kind(nil) != "" || typescript.AsNode((*typescript.Identifier)(nil)) != nil {
		t.Error("nil node")
	}
}

// FuzzParse checks that ParseAST and Recognize agree on any input, that a successful parse has valid ranges, and
// that nothing panics: go test -fuzz FuzzParse
func FuzzParse(f *testing.F) {
	for _, s := range valid {
		f.Add(s)
	}
	for _, s := range validTSX {
		f.Add("\x00" + s)
	}
	for _, s := range invalid {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		var file *typescript.SourceFile
		var err, rerr error
		if strings.HasPrefix(src, "\x00") {
			file, err = typescript.ParseTSX(src[1:])
			rerr = typescript.RecognizeTSX(src[1:])
		} else {
			file, err = typescript.ParseAST(src)
			rerr = typescript.Recognize(src)
		}
		if (err == nil) != (rerr == nil) {
			t.Fatalf("ParseAST error %v, Recognize error %v", err, rerr)
		}
		if err == nil {
			n := len([]rune(src))
			typescript.Inspect(file, func(node typescript.ASTNode) bool {
				if node != nil {
					if s, e := node.Range(); s < 0 || e < s || e > n+1 {
						t.Fatalf("%s has range %d-%d in %d code points", typescript.Kind(node), s, e, n)
					}
				}
				return true
			})
		}
	})
}
