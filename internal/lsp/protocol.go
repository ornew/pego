package lsp

// The subset of the Language Server Protocol (version 3.17) types that the server uses.

// Position is a position in a text document: a zero-based line and a zero-based offset in UTF-16
// code units in that line.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a range in a text document; End is exclusive.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is a range in a document.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type textDocumentIdentifier struct {
	URI string `json:"uri"`
}

type textDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type versionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

type textDocumentPositionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type didOpenParams struct {
	TextDocument textDocumentItem `json:"textDocument"`
}

// contentChange is a change of a document: Text replaces Range, or the whole document if Range is
// nil.
type contentChange struct {
	Range *Range `json:"range"`
	Text  string `json:"text"`
}

type didChangeParams struct {
	TextDocument   versionedTextDocumentIdentifier `json:"textDocument"`
	ContentChanges []contentChange                 `json:"contentChanges"`
}

type didSaveParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Text         *string                `json:"text"`
}

type didCloseParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

// severityError is the severity of diagnostics (all of them are errors).
const severityError = 1

// Diagnostic is a problem in a document.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Source   string `json:"source"`
	Message  string `json:"message"`
}

type publishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     *int         `json:"version,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// TextEdit replaces Range with NewText.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

type documentFormattingParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type referenceParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      struct {
		IncludeDeclaration bool `json:"includeDeclaration"`
	} `json:"context"`
}

type renameParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	NewName      string                 `json:"newName"`
}

type workspaceEdit struct {
	Changes map[string][]TextEdit `json:"changes"`
}

type prepareRenameResult struct {
	Range       Range  `json:"range"`
	Placeholder string `json:"placeholder"`
}

// Symbol kinds.
const (
	symbolKindClass     = 5
	symbolKindField     = 8
	symbolKindInterface = 11
	symbolKindFunction  = 12
	symbolKindString    = 15
	symbolKindStruct    = 23
)

// DocumentSymbol is a symbol of a document outline.
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

type markupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type hover struct {
	Contents markupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

// Completion item kinds.
const (
	completionKindFunction = 3
	completionKindField    = 5
	completionKindVariable = 6
	completionKindClass    = 7
	completionKindProperty = 10
	completionKindKeyword  = 14
	completionKindStruct   = 22
)

// insertSnippet is the format of insert text in the snippet syntax.
const insertSnippet = 2

// CompletionItem is a completion proposal.
type CompletionItem struct {
	Label            string         `json:"label"`
	Kind             int            `json:"kind,omitempty"`
	Detail           string         `json:"detail,omitempty"`
	Documentation    *markupContent `json:"documentation,omitempty"`
	SortText         string         `json:"sortText,omitempty"`
	InsertText       string         `json:"insertText,omitempty"`
	InsertTextFormat int            `json:"insertTextFormat,omitempty"`
}

type completionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

// Document highlight kinds.
const (
	highlightRead  = 2
	highlightWrite = 3
)

type documentHighlight struct {
	Range Range `json:"range"`
	Kind  int   `json:"kind"`
}
