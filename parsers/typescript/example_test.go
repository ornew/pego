package typescript_test

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ornew/pego/parsers/typescript"
)

func ExampleParseAST() {
	src := `export const answer: number = 6 * 7;`
	f, err := typescript.ParseAST(src)
	if err != nil {
		panic(err)
	}
	stmt := f.Statements[0].(*typescript.VariableStatement)
	decl := stmt.DeclarationList.Declarations[0]
	text := func(n any) string { // the fields have union types, which AsNode turns into nodes
		start, end := typescript.AsNode(n).Range()
		return src[start:end]
	}
	fmt.Printf("%s: %s = %s\n", text(decl.Name), text(decl.Type), text(decl.Initializer))
	fmt.Println(stmt.DeclarationList.Keyword, typescript.Kind(stmt.Modifiers[0]))
	// Output:
	// answer: number = 6 * 7
	// const ExportKeyword
}

// Every node has the kind and the range of the TypeScript compiler's node, and its children in the order of
// the compiler's forEachChild.
func ExampleInspect() {
	src := "const f = (a: string) => a.length > 0 ? `yes` : null;"
	f, err := typescript.ParseAST(src)
	if err != nil {
		panic(err)
	}
	depth := 0
	typescript.Inspect(f, func(n typescript.ASTNode) bool {
		if n == nil { // after the children of a node
			depth--
			return true
		}
		start, end := n.Range()
		text := src[start:end]
		if len(text) > 24 {
			text = text[:21] + "..."
		}
		fmt.Printf("%s%s %d-%d %q\n", strings.Repeat("  ", depth), typescript.Kind(n), start, end, text)
		depth++
		return true
	})
	// Output:
	// SourceFile 0-53 "const f = (a: string)..."
	//   VariableStatement 0-53 "const f = (a: string)..."
	//     VariableDeclarationList 0-52 "const f = (a: string)..."
	//       VariableDeclaration 6-52 "f = (a: string) => a...."
	//         Identifier 6-7 "f"
	//         ArrowFunction 10-52 "(a: string) => a.leng..."
	//           Parameter 11-20 "a: string"
	//             Identifier 11-12 "a"
	//             StringKeyword 14-20 "string"
	//           EqualsGreaterThanToken 22-24 "=>"
	//           ConditionalExpression 25-52 "a.length > 0 ? `yes` ..."
	//             BinaryExpression 25-37 "a.length > 0"
	//               PropertyAccessExpression 25-33 "a.length"
	//                 Identifier 25-26 "a"
	//                 Identifier 27-33 "length"
	//               GreaterThanToken 34-35 ">"
	//               NumericLiteral 36-37 "0"
	//             QuestionToken 38-39 "?"
	//             NoSubstitutionTemplateLiteral 40-45 "`yes`"
	//             ColonToken 46-47 ":"
	//             NullKeyword 48-52 "null"
	//   EndOfFileToken 53-53 ""
}

func ExampleParseTSX() {
	// <T>(x: T) is a type assertion in a .ts file and a generic arrow function or JSX in a .tsx file.
	f, err := typescript.ParseTSX("const el = <ul className=\"list\">{items.map((i) => <li key={i}>{i}</li>)}</ul>;")
	if err != nil {
		panic(err)
	}
	n := 0
	typescript.Inspect(f, func(node typescript.ASTNode) bool {
		if _, ok := node.(*typescript.JsxElement); ok {
			n++
		}
		return true
	})
	fmt.Println(n, "JSX elements")
	// Output: 2 JSX elements
}

// ParseFile chooses TS or TSX by the file name, and parses the top level of a module in the await context, as
// ts.createSourceFile does.
func ExampleParseFile() {
	const src = "import fs from 'fs'; const data = await (read());"
	f, err := typescript.ParseFile("main.ts", src)
	if err != nil {
		panic(err)
	}
	fmt.Println(typescript.IsExternalModule(f))
	_, isAwait := f.Statements[1].(*typescript.VariableStatement).DeclarationList.Declarations[0].Initializer.(*typescript.AwaitExpression)
	fmt.Println(isAwait)
	// Output:
	// true
	// true
}

func ExampleSyntaxError() {
	_, err := typescript.ParseAST("const a = 1;\nconst b = ;\n")
	var se *typescript.SyntaxError
	if errors.As(err, &se) {
		fmt.Println(se.Line, se.Col, se.Pos, se.Message())
	}
	// Output: 2 11 23 expected an expression
}

func ExampleRecognize() {
	fmt.Println(typescript.Recognize("interface A { x: number }"))
	fmt.Println(typescript.Recognize("interface A { x: number") != nil)
	fmt.Println(typescript.RecognizeTSX("let a = <T,>(x: T) => x;"))
	// Output:
	// <nil>
	// true
	// <nil>
}
