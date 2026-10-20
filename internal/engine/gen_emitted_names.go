package engine

import "strings"

// emittedRuntimeNames are the exports of the runtime that the Go generators write into parser.go,
// in code, templates and type expressions, as unqualified names (qualifyRuntime resolves them to
// the runtime's package). Types of a typed grammar must not take them: a type of that name would
// capture the references. The set is fixed, so the Go name of a type does not depend on which of
// the names the output of a particular grammar uses. The names the generated package declares on
// purpose (Error, TNode, Document, ParseStats, Span) are in designedRuntimeNames instead.
//
// TestEmittedRuntimeNames checks the list against the string literals of the generators, and
// generate fails if the code refers to a runtime export that the same code declares.
var emittedRuntimeNames = strings.Fields(`
	ActCtx AsTval BaseString Bytes CaptureFrame CodePoints DocumentBusy
	DocumentParseConverted DocumentSliceKey EmptyFrame EmptyTFrame Env ExpID ExpMark
	FieldSpanOf FieldType IdAny IdBeginInput IdBeginLine IdEndInput IdEndLine IterateStream
	KindOf MakeDocument Mark MsgBit NewGrammar NewNode NewNodeTypeKinds NewStreamIterator
	NewTError Node OperatorsRule OperatorsRuleLine OperatorsRuleOp ParseOption Parser Profile
	RawKindList RawKindMatch RawKindSeq RenamedExpected ResolveOptions Rule RunParse
	RunParseConverted RunParseResolved RunParseStream RunParseStreamWork Shaper
	StreamIterator StreamPull SyntaxError SyntaxErrors TActCtx TAs TBack TCaptureFrame TDeref
	TError TFreshNode TKid TListOf TMatch TMatchAs TOperand TOperatorsRule TOperatorsRuleLine
	TOperatorsRuleOp TParse TParseResolved TParser TPtr TPubError TPubHooks TPubIn TPubMatch
	TPubNodeIn TRegisterNodeType TRule TSeq TSetNodeType TVal TraceEvent TypeListValue
	TypeTupleValue TypedDocumentCloneBase TypedDocumentCloneTask TypedDocumentParse UnboxNode
	UnboxValue WorkControl WorkControlsEnabled WorkRange`)
