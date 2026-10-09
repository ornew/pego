package syntax

import (
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
)

// This file exposes the lexer and an error-tolerant parse to tools that work on source as it is
// being edited, such as the language server (internal/lsp).

// TokenKind is the kind of a Token.
type TokenKind int

// Kinds of tokens.
const (
	TokenIdent     TokenKind = iota + 1 // identifier or keyword
	TokenString                         // "..."
	TokenInt                            // 123
	TokenCharClass                      // (?...)
	TokenCapture                        // $name
	TokenIndex                          // $1
	TokenPunct                          // punctuation
	TokenComment                        // // ...
)

// Token is a lexical token of PEGO source.
type Token struct {
	Kind TokenKind
	// Text is the name of an identifier or a capture reference, the punctuation, the digits of an
	// integer or an index reference (preserving spelling even when invalid), the
	// decoded value of a string, or the text of a comment
	// (including "//", without trailing white space). It is empty for a character class.
	Text string
	// Pos is the position of the first character of the token, and End the position just after
	// its last character.
	Pos, End grammar.Pos
}

// Tokenize splits src into tokens, including comments, in source order. Characters that do not
// start a token are skipped, so Tokenize never fails; Parse reports them.
func Tokenize(src string) []Token {
	var out []Token
	comment := func(c *grammar.Comment) {
		end := c.Pos
		end.Col += utf8.RuneCountInString(c.Text)
		out = append(out, Token{Kind: TokenComment, Text: c.Text, Pos: c.Pos, End: end})
	}
	for _, t := range lex(src, &errorList{}) {
		if t.lineComment != nil {
			comment(t.lineComment)
		}
		for _, c := range t.comments {
			comment(c)
		}
		tok := Token{Pos: t.pos, End: t.end, Text: t.text}
		switch t.kind {
		case tEOF:
			return out
		case tIdent:
			tok.Kind = TokenIdent
		case tString:
			tok.Kind = TokenString
		case tInt:
			tok.Kind = TokenInt
		case tCharClass:
			tok.Kind = TokenCharClass
		case tCapture:
			tok.Kind = TokenCapture
		case tIndex:
			tok.Kind = TokenIndex
		case tPunct:
			tok.Kind = TokenPunct
		}
		out = append(out, tok)
	}
	return out
}

// ParsePartial parses src like Parse, but returns the grammar even when there are errors. After an
// error the parser skips to the next def or type, so the grammar holds the definitions that parsed
// to the end; a definition with an error in the middle is missing. Errors that do not stop the
// parser of a definition, such as an invalid escape sequence, leave the definition in the grammar.
func ParsePartial(src string) (*grammar.Grammar, ErrorList) {
	errs := &errorList{}
	p := &parser{toks: lex(src, errs), errs: errs}
	g := p.file()
	return g, errs.list
}

// IsKeyword reports whether name is a keyword, which cannot name a rule.
func IsKeyword(name string) bool { return keywords[name] }

// IsIdentifier reports whether s is an identifier: a letter or "_" followed by letters, digits
// and "_".
func IsIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == utf8.RuneError || i == 0 && !isIdentStart(r) || !isIdentPart(r) {
			return false
		}
	}
	return true
}
