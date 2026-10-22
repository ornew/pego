package syntax

import (
	"context"
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
	TokenCharClass                      // [...]
	TokenCapture                        // $name
	TokenIndex                          // $1
	TokenPunct                          // punctuation
	TokenComment                        // // ... or /* ... */
)

// Token is a lexical token of PEGO source.
type Token struct {
	Kind TokenKind
	// Text is the name of an identifier or a capture reference, the punctuation, the digits of an
	// integer (including the signed minimum int) or an index reference
	// (preserving spelling even when invalid), the
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
	tokens, _ := tokenizeContext(nil, src)
	return tokens
}

// TokenizeContext is Tokenize with cancellation. It returns no tokens when
// cancelled, including when cancellation occurs in the middle of a token.
func TokenizeContext(ctx context.Context, src string) ([]Token, error) {
	return tokenizeContext(ctx, src)
}

func tokenizeContext(ctx context.Context, src string) ([]Token, error) {
	var out []Token
	comment := func(c *grammar.Comment) {
		end := c.End
		if end == (grammar.Pos{}) {
			end = c.Pos
			end.Col += utf8.RuneCountInString(c.Text)
		}
		out = append(out, Token{Kind: TokenComment, Text: c.Text, Pos: c.Pos, End: end})
	}
	toks, err := lexContext(ctx, src, &errorList{})
	if err != nil {
		return nil, err
	}
	for i, t := range toks {
		if ctx != nil && i&255 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if t.lineComment != nil {
			comment(t.lineComment)
		}
		for j, c := range t.comments {
			if ctx != nil && j&255 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			comment(c)
		}
		tok := Token{Pos: t.pos, End: t.end, Text: t.text}
		switch t.kind {
		case tEOF:
			if ctx != nil {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			return out, nil
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
	return out, nil
}

// ParsePartial parses src like Parse, but returns the grammar even when there are errors. After an
// error the parser skips to the next rule or type, so the grammar holds the definitions that parsed
// to the end; a definition with an error in the middle is missing. Errors that do not stop the
// parser of a definition, such as an invalid escape sequence, leave the definition in the grammar.
func ParsePartial(src string) (*grammar.Grammar, ErrorList) {
	g, errs, _ := parsePartialContext(nil, src)
	return g, errs
}

// ParsePartialContext is ParsePartial with cancellation during lexing and
// parsing. Cancelled calls return no grammar or diagnostics.
func ParsePartialContext(ctx context.Context, src string) (*grammar.Grammar, ErrorList, error) {
	return parsePartialContext(ctx, src)
}

func parsePartialContext(ctx context.Context, src string) (g *grammar.Grammar, diagnostics ErrorList, parseErr error) {
	defer func() {
		if value := recover(); value != nil {
			if cancelled, ok := value.(parseCancelled); ok {
				g, diagnostics, parseErr = nil, nil, cancelled.err
				return
			}
			panic(value)
		}
	}()
	errs := &errorList{}
	toks, err := lexContext(ctx, src, errs)
	if err != nil {
		return nil, nil, err
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
	}
	p := &parser{toks: toks, errs: errs, ctx: ctx}
	g = p.file()
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
	}
	return g, errs.list, nil
}

// IsRuleName reports whether s can name a rule: an identifier, or "_" followed by ASCII letters,
// digits and "_", which makes a hidden rule. The name "_" alone is not a rule name.
func IsRuleName(s string) bool {
	if len(s) < 2 || s[0] != '_' {
		return IsIdentifier(s)
	}
	for _, r := range s {
		if r == utf8.RuneError || !isIdentPart(r) {
			return false
		}
	}
	return true
}

// IsKeyword reports whether name can act as a contextual keyword. These words
// remain valid identifiers outside their keyword positions.
func IsKeyword(name string) bool { return keywords[name] }

// IsIdentifier reports whether s is an available ASCII identifier: an A-Z or
// a-z letter followed by ASCII letters, digits and "_". Names beginning with
// "_" are not identifiers; only a rule name may begin with one (see IsRuleName).
func IsIdentifier(s string) bool {
	if s == "" || s[0] == '_' {
		return false
	}
	for i, r := range s {
		if r == utf8.RuneError || i == 0 && !isIdentStart(r) || !isIdentPart(r) {
			return false
		}
	}
	return true
}
