package typescript

import (
	"reflect"
	"sync"
)

// ASTNode is a node of the AST: a pointer to one of the struct or terminal types of this package, each of which
// embeds Span, its range in the input. Kind gives its SyntaxKind, and ForEachChild and Inspect visit its
// children in the order of the TypeScript compiler's forEachChild.
type ASTNode interface {
	Range() (start, end int)
	tspan() (int, int) // the generated method of *Span: only the types of this package are nodes
}

// Range returns the start and the end (exclusive) of the node in the input.
func (s Span) Range() (start, end int) { return s.Start, s.End }

// Kind returns the name of the node's SyntaxKind in the TypeScript compiler, such as "Identifier",
// "CallExpression", "PlusToken" or "ExportKeyword". Most types are named after their kind; the terminals that
// stand for several kinds (Token, Modifier, KeywordTypeNode, BooleanLiteral) have the kind of their text.
func Kind(n ASTNode) string {
	switch n := n.(type) {
	case *Token:
		return tokenKinds[n.Text]
	case *Modifier:
		return tokenKinds[n.Text]
	case *KeywordTypeNode:
		return tokenKinds[n.Text]
	case *BooleanLiteral:
		return tokenKinds[n.Text]
	case *ThisExpression:
		return "ThisKeyword"
	case *SuperExpression:
		return "SuperKeyword"
	case *NullLiteral:
		return "NullKeyword"
	case *ImportExpression:
		return "ImportKeyword"
	case *ThisTypeNode:
		return "ThisType"
	case nil:
		return ""
	}
	return reflect.TypeOf(n).Elem().Name()
}

// tokenKinds maps the text of a token, modifier or keyword to its SyntaxKind.
var tokenKinds = map[string]string{
	"{": "OpenBraceToken", "}": "CloseBraceToken", "(": "OpenParenToken", ")": "CloseParenToken",
	"[": "OpenBracketToken", "]": "CloseBracketToken", ".": "DotToken", "...": "DotDotDotToken",
	";": "SemicolonToken", ",": "CommaToken", "?.": "QuestionDotToken", "<": "LessThanToken",
	"</": "LessThanSlashToken", ">": "GreaterThanToken", "<=": "LessThanEqualsToken",
	">=": "GreaterThanEqualsToken", "==": "EqualsEqualsToken", "!=": "ExclamationEqualsToken",
	"===": "EqualsEqualsEqualsToken", "!==": "ExclamationEqualsEqualsToken", "=>": "EqualsGreaterThanToken",
	"+": "PlusToken", "-": "MinusToken", "*": "AsteriskToken", "**": "AsteriskAsteriskToken", "/": "SlashToken",
	"%": "PercentToken", "++": "PlusPlusToken", "--": "MinusMinusToken", "<<": "LessThanLessThanToken",
	">>": "GreaterThanGreaterThanToken", ">>>": "GreaterThanGreaterThanGreaterThanToken", "&": "AmpersandToken",
	"|": "BarToken", "^": "CaretToken", "!": "ExclamationToken", "~": "TildeToken",
	"&&": "AmpersandAmpersandToken", "||": "BarBarToken", "?": "QuestionToken", ":": "ColonToken", "@": "AtToken",
	"??": "QuestionQuestionToken", "#": "HashToken", "=": "EqualsToken", "+=": "PlusEqualsToken",
	"-=": "MinusEqualsToken", "*=": "AsteriskEqualsToken", "**=": "AsteriskAsteriskEqualsToken",
	"/=": "SlashEqualsToken", "%=": "PercentEqualsToken", "<<=": "LessThanLessThanEqualsToken",
	">>=": "GreaterThanGreaterThanEqualsToken", ">>>=": "GreaterThanGreaterThanGreaterThanEqualsToken",
	"&=": "AmpersandEqualsToken", "|=": "BarEqualsToken", "||=": "BarBarEqualsToken",
	"&&=": "AmpersandAmpersandEqualsToken", "??=": "QuestionQuestionEqualsToken", "^=": "CaretEqualsToken",

	"break": "BreakKeyword", "case": "CaseKeyword", "catch": "CatchKeyword", "class": "ClassKeyword",
	"const": "ConstKeyword", "continue": "ContinueKeyword", "debugger": "DebuggerKeyword",
	"default": "DefaultKeyword", "delete": "DeleteKeyword", "do": "DoKeyword", "else": "ElseKeyword",
	"enum": "EnumKeyword", "export": "ExportKeyword", "extends": "ExtendsKeyword", "false": "FalseKeyword",
	"finally": "FinallyKeyword", "for": "ForKeyword", "function": "FunctionKeyword", "if": "IfKeyword",
	"import": "ImportKeyword", "in": "InKeyword", "instanceof": "InstanceOfKeyword", "new": "NewKeyword",
	"null": "NullKeyword", "return": "ReturnKeyword", "super": "SuperKeyword", "switch": "SwitchKeyword",
	"this": "ThisKeyword", "throw": "ThrowKeyword", "true": "TrueKeyword", "try": "TryKeyword",
	"typeof": "TypeOfKeyword", "var": "VarKeyword", "void": "VoidKeyword", "while": "WhileKeyword",
	"with": "WithKeyword", "implements": "ImplementsKeyword", "interface": "InterfaceKeyword",
	"let": "LetKeyword", "package": "PackageKeyword", "private": "PrivateKeyword",
	"protected": "ProtectedKeyword", "public": "PublicKeyword", "static": "StaticKeyword",
	"yield": "YieldKeyword", "abstract": "AbstractKeyword", "accessor": "AccessorKeyword", "as": "AsKeyword",
	"asserts": "AssertsKeyword", "assert": "AssertKeyword", "any": "AnyKeyword", "async": "AsyncKeyword",
	"await": "AwaitKeyword", "boolean": "BooleanKeyword", "constructor": "ConstructorKeyword",
	"declare": "DeclareKeyword", "get": "GetKeyword", "infer": "InferKeyword", "intrinsic": "IntrinsicKeyword",
	"is": "IsKeyword", "keyof": "KeyOfKeyword", "module": "ModuleKeyword", "namespace": "NamespaceKeyword",
	"never": "NeverKeyword", "out": "OutKeyword", "readonly": "ReadonlyKeyword", "require": "RequireKeyword",
	"number": "NumberKeyword", "object": "ObjectKeyword", "satisfies": "SatisfiesKeyword", "set": "SetKeyword",
	"string": "StringKeyword", "symbol": "SymbolKeyword", "type": "TypeKeyword", "undefined": "UndefinedKeyword",
	"unique": "UniqueKeyword", "unknown": "UnknownKeyword", "using": "UsingKeyword", "from": "FromKeyword",
	"global": "GlobalKeyword", "bigint": "BigIntKeyword", "override": "OverrideKeyword", "of": "OfKeyword",
	"defer": "DeferKeyword",
}

// ForEachChild calls f for each child of n, in the order of the TypeScript compiler's forEachChild, and stops
// when f returns false. It returns false if f did. Children are the node-valued fields of n, and the elements
// of its list fields; attributes such as the operator of a PrefixUnaryExpression are not children.
func ForEachChild(n ASTNode, f func(ASTNode) bool) bool {
	if n == nil {
		return true
	}
	v := reflect.ValueOf(n)
	if v.IsNil() {
		return true
	}
	v = v.Elem()
	for _, i := range childFields(v.Type()) {
		fv := v.Field(i)
		switch fv.Kind() {
		case reflect.Slice:
			for j := range fv.Len() {
				if c, ok := asASTNode(fv.Index(j)); ok && !f(c) {
					return false
				}
			}
		default:
			if c, ok := asASTNode(fv); ok && !f(c) {
				return false
			}
		}
	}
	return true
}

// Inspect visits n and its descendants in depth-first order, the children of a node in the order of
// ForEachChild: it calls f(n), and if that returns true, inspects each child, then calls f(nil).
func Inspect(n ASTNode, f func(ASTNode) bool) {
	if n == nil || reflect.ValueOf(n).IsNil() {
		return
	}
	if !f(n) {
		return
	}
	ForEachChild(n, func(c ASTNode) bool {
		Inspect(c, f)
		return true
	})
	f(nil)
}

func asASTNode(v reflect.Value) (ASTNode, bool) {
	if v.IsNil() {
		return nil, false
	}
	n, ok := v.Interface().(ASTNode)
	return n, ok
}

var astNodeType = reflect.TypeFor[ASTNode]()

// childFields returns the indices of the fields of the struct type t that hold nodes or lists of nodes.
func childFields(t reflect.Type) []int {
	if f, ok := fieldCache.Load(t); ok {
		return f.([]int)
	}
	var fields []int
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Anonymous {
			continue
		}
		ft := f.Type
		if ft.Kind() == reflect.Slice {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Interface || ft.Kind() == reflect.Pointer && ft.Implements(astNodeType) {
			fields = append(fields, i)
		}
	}
	fieldCache.Store(t, fields)
	return fields
}

var fieldCache sync.Map
