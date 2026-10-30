package syntax

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ornew/pego/grammar"
)

// cancelOnCheck cancels at a known polling point, so the test does not rely on
// scheduling a goroutine during a fast lexing pass.
type cancelOnCheck struct {
	checks   int
	cancelAt int
}

func (*cancelOnCheck) Deadline() (deadline time.Time, ok bool) { return }
func (*cancelOnCheck) Done() <-chan struct{}                   { return nil }
func (*cancelOnCheck) Value(any) any                           { return nil }
func (c *cancelOnCheck) Err() error {
	c.checks++
	if c.checks >= c.cancelAt {
		return context.Canceled
	}
	return nil
}

func TestCancellableLexPreCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if toks, err := TokenizeContext(ctx, "rule main = \"x\""); toks != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("TokenizeContext = %v, %v", toks, err)
	}
	if g, errs, err := ParsePartialContext(ctx, "rule main = \"x\""); g != nil || errs != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("ParsePartialContext = %v, %v, %v", g, errs, err)
	}
}

func TestCancellableLexWithinLongRuns(t *testing.T) {
	for _, tc := range []struct {
		name, source string
	}{
		{"identifier", strings.Repeat("a", 8192)},
		{"number", strings.Repeat("1", 8192)},
		{"string", `"` + strings.Repeat("x", 8192) + `"`},
		{"class", `[` + strings.Repeat("x", 8192) + `]`},
		{"line comment", `//` + strings.Repeat("x", 8192)},
		{"block comment", `/*` + strings.Repeat("x", 8192) + `*/`},
		{"invalid run", strings.Repeat("`", 8192)},
		{"unicode escape", `"\u{` + strings.Repeat("a", 8192) + `}"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The first checks cover preflight, rune decoding, and the loop
			// boundary. The next one occurs inside the single long run.
			ctx := &cancelOnCheck{cancelAt: 4 + 2*(len([]rune(tc.source))/256)}
			got, err := lexContext(ctx, tc.source, &errorList{})
			if got != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("lexContext returned %d tokens, %v after %d polls", len(got), err, ctx.checks)
			}
			if ctx.checks != ctx.cancelAt {
				t.Fatalf("cancelled after %d polls, want %d", ctx.checks, ctx.cancelAt)
			}
			ctx = &cancelOnCheck{cancelAt: 4 + 2*(len([]rune(tc.source))/256)}
			if tokens, err := TokenizeContext(ctx, tc.source); tokens != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("TokenizeContext returned %d tokens, %v", len(tokens), err)
			}
			ctx = &cancelOnCheck{cancelAt: 4 + 2*(len([]rune(tc.source))/256)}
			if grammar, diagnostics, err := ParsePartialContext(ctx, tc.source); grammar != nil || diagnostics != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("ParsePartialContext returned %v, %v, %v", grammar, diagnostics, err)
			}
		})
	}
}

func TestCancellableLexMatchesOrdinaryAPIs(t *testing.T) {
	src := "// 😀\r\nrule main = [é-ê] / \"x😀\\n\" /* 外\r\n/* 内 */ 終 */\r\nrule next = \"y\" // tail\r"
	wantLexErrs := &errorList{}
	wantLex := lex(src, wantLexErrs)
	gotLexErrs := &errorList{}
	gotLex, err := lexContext(context.Background(), src, gotLexErrs)
	if err != nil || !reflect.DeepEqual(gotLex, wantLex) || !reflect.DeepEqual(gotLexErrs, wantLexErrs) {
		t.Fatalf("lex mismatch: error=%v, tokens equal=%v, diagnostics equal=%v", err, reflect.DeepEqual(gotLex, wantLex), reflect.DeepEqual(gotLexErrs, wantLexErrs))
	}
	gotTokens, err := TokenizeContext(context.Background(), src)
	if err != nil || !reflect.DeepEqual(gotTokens, Tokenize(src)) {
		t.Fatalf("token mismatch: error=%v, tokens equal=%v", err, reflect.DeepEqual(gotTokens, Tokenize(src)))
	}
	gotGrammar, gotErrs, err := ParsePartialContext(context.Background(), src)
	wantGrammar, wantErrs := ParsePartial(src)
	if err != nil || !reflect.DeepEqual(gotGrammar, wantGrammar) || !reflect.DeepEqual(gotErrs, wantErrs) {
		t.Fatalf("partial parse mismatch: error=%v, grammar equal=%v, diagnostics equal=%v", err, reflect.DeepEqual(gotGrammar, wantGrammar), reflect.DeepEqual(gotErrs, wantErrs))
	}
}

func TestCancellableRuneDecodeCapacity(t *testing.T) {
	for _, tc := range []struct {
		name, source string
	}{
		{"ASCII", strings.Repeat("a", 8192)},
		{"multibyte", strings.Repeat("😀", 8192)},
		{"mixed", strings.Repeat("a😀", 4096)},
		{"invalid UTF-8", strings.Repeat("\xff", 8192)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := &lexer{ctx: context.Background()}
			decoded := l.decodeRunes(tc.source)
			want := []rune(tc.source)
			if !reflect.DeepEqual(decoded, want) || cap(decoded) != len(want) {
				t.Fatalf("decoded %d runes with capacity %d; want %d runes and capacity", len(decoded), cap(decoded), len(want))
			}
		})
	}
}

func TestCancellableWhitespaceBlockComment(t *testing.T) {
	comment := "/*\n" + strings.Repeat(" ", 8192) + "x*/"
	source := comment + "\nrule main = \"a\""
	wantErrs := &errorList{}
	want := lex(source, wantErrs)
	gotErrs := &errorList{}
	got, err := lexContext(context.Background(), source, gotErrs)
	if err != nil || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gotErrs, wantErrs) {
		t.Fatalf("whitespace comment parity: error=%v, tokens equal=%v, diagnostics equal=%v", err, reflect.DeepEqual(got, want), reflect.DeepEqual(gotErrs, wantErrs))
	}
	if len(got) == 0 || len(got[0].comments) != 1 || got[0].comments[0].Content != "\nx" {
		t.Fatalf("unexpected normalized comment: %+v", got)
	}
	runes := []rune(comment)
	// Stop in the second line's whitespace trimming, after scanning and
	// converting the full source comment.
	ctx := &cancelOnCheck{cancelAt: 102}
	l := &lexer{ctx: ctx, src: runes, srcASCII: true, line: 1, col: 1, errs: &errorList{}}
	expectLexCancelled(t, func() { l.skipSpace() })
	if l.i != len(runes) || ctx.checks != ctx.cancelAt {
		t.Fatalf("cancelled before whitespace normalization: offset=%d/%d checks=%d/%d", l.i, len(runes), ctx.checks, ctx.cancelAt)
	}
}

func TestCancellableUnicodeCommentConversion(t *testing.T) {
	for _, comment := range []string{
		"//" + strings.Repeat("😀", 8192),
		"//" + strings.Repeat("\xff", 8192),
	} {
		wantErrs := &errorList{}
		want := lex(comment, wantErrs)
		gotErrs := &errorList{}
		got, err := lexContext(context.Background(), comment, gotErrs)
		if err != nil || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gotErrs, wantErrs) {
			t.Fatalf("comment conversion mismatch: error=%v, tokens equal=%v, diagnostics equal=%v", err, reflect.DeepEqual(got, want), reflect.DeepEqual(gotErrs, wantErrs))
		}
	}
	runes := []rune("//" + strings.Repeat("😀", 8192))
	// Stop during the UTF-8 byte count, after consuming the entire comment.
	ctx := &cancelOnCheck{cancelAt: len(runes)/256 + 2}
	l := &lexer{ctx: ctx, src: runes, line: 1, col: 1, errs: &errorList{}}
	expectLexCancelled(t, func() { l.skipSpace() })
	if l.i != len(runes) || ctx.checks != ctx.cancelAt {
		t.Fatalf("cancelled before byte count: offset=%d/%d checks=%d/%d", l.i, len(runes), ctx.checks, ctx.cancelAt)
	}
}

func expectLexCancelled(t *testing.T, run func()) {
	t.Helper()
	defer func() {
		x := recover()
		cancelled, ok := x.(lexCancelled)
		if !ok || !errors.Is(cancelled.err, context.Canceled) {
			t.Fatalf("wanted lex cancellation, got %v", x)
		}
	}()
	run()
}

func TestCancellableCommentPostScanBudget(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		// Extra checks performed while normalizing the comment content.
		contentChecks int
	}{
		{"block normalization", "/*" + strings.Repeat("x", 8192) + "*/", 0},
		{"line trimming", "//" + strings.Repeat("x", 8192) + strings.Repeat(" ", 8192), 64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runes := []rune(tc.source)
			scanChecks := len(runes) / 256
			stringChecks := (len(runes) + 255) / 256
			ctx := &cancelOnCheck{cancelAt: scanChecks + stringChecks + tc.contentChecks + 1}
			l := &lexer{ctx: ctx, src: runes, srcASCII: true, line: 1, col: 1, errs: &errorList{}}
			expectLexCancelled(t, func() { l.skipSpace() })
			if l.i != len(runes) || ctx.checks != ctx.cancelAt {
				t.Fatalf("cancelled before post-scan work: offset=%d/%d checks=%d/%d", l.i, len(runes), ctx.checks, ctx.cancelAt)
			}
		})
	}
}

func TestCancellableCommentCollection(t *testing.T) {
	const count = 8192
	comments := make([]*grammar.Comment, count)
	for i := range comments {
		comments[i] = &grammar.Comment{Pos: grammar.Pos{Line: i + 1, Col: 1}, End: grammar.Pos{Line: i + 1, Col: 4}, Text: "//x", Content: "x\n"}
	}
	// Poll in both the collection scan and the length pass, then stop
	// while copying the joined comment contents.
	ctx := &cancelOnCheck{cancelAt: 2*(count/256) + 2}
	next := token{kind: tEOF, pos: grammar.Pos{Line: count + 1, Col: 1}}
	expectLexCancelled(t, func() {
		collectCommentsWithCheck(grammar.Pos{}, false, false, next, comments, func() {
			if err := ctx.Err(); err != nil {
				panic(lexCancelled{err})
			}
		})
	})
	if ctx.checks != ctx.cancelAt {
		t.Fatalf("collection stopped after %d polls, want %d", ctx.checks, ctx.cancelAt)
	}
}

func TestCancellableTokenizeCommentExpansion(t *testing.T) {
	src := strings.Repeat("//x\n", 2048)
	baseline := &cancelOnCheck{cancelAt: int(^uint(0) >> 1)}
	lexed, err := lexContext(baseline, src, &errorList{})
	if err != nil || len(lexed) != 1 || len(lexed[0].comments) != 2048 {
		t.Fatalf("lexed %d tokens, %d comments, %v", len(lexed), len(lexed[0].comments), err)
	}
	// One poll at the EOF token, one at the first comment, and the next
	// inside the expansion of that token's comment list.
	ctx := &cancelOnCheck{cancelAt: baseline.checks + 3}
	got, err := TokenizeContext(ctx, src)
	if got != nil || !errors.Is(err, context.Canceled) || ctx.checks != ctx.cancelAt {
		t.Fatalf("TokenizeContext returned %d tokens, %v after %d polls", len(got), err, ctx.checks)
	}
}

func TestCancellableLongTokenConversion(t *testing.T) {
	for _, tc := range []struct {
		name, source string
	}{
		{"identifier", strings.Repeat("a", 8192)},
		{"capture", "$" + strings.Repeat("a", 8192)},
		{"number", strings.Repeat("1", 8192)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runes := []rune(tc.source)
			ctx := &cancelOnCheck{cancelAt: len(runes)/256 + 1}
			l := &lexer{ctx: ctx, src: runes, srcASCII: true, line: 1, col: 1, errs: &errorList{}}
			expectLexCancelled(t, func() { l.next() })
			if l.i != len(runes) || ctx.checks != ctx.cancelAt {
				t.Fatalf("cancelled before conversion: offset=%d/%d checks=%d/%d", l.i, len(runes), ctx.checks, ctx.cancelAt)
			}
		})
	}
}

func TestCancellableLongNumericBudget(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		wantOffset   int
	}{
		{"decimal value", strings.Repeat("0", 8192), 8192},
		{"unicode escape value", `"\u{` + strings.Repeat("0", 8192) + `}"`, 8197},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runes := []rune(tc.source)
			// The scan polls 32 times. A decimal string conversion polls
			// 32 more times; the escape builds its hex text during scanning.
			cancelAt := len(runes)/256 + 2
			if tc.name == "decimal value" {
				cancelAt += (len(runes) + 255) / 256
			}
			ctx := &cancelOnCheck{cancelAt: cancelAt}
			l := &lexer{ctx: ctx, src: runes, srcASCII: true, line: 1, col: 1, errs: &errorList{}}
			expectLexCancelled(t, func() { l.next() })
			if l.i != tc.wantOffset || ctx.checks != ctx.cancelAt {
				t.Fatalf("cancelled before numeric parse: offset=%d/%d checks=%d/%d", l.i, tc.wantOffset, ctx.checks, ctx.cancelAt)
			}
		})
	}
}

func TestCancellableLongNumericParity(t *testing.T) {
	for _, src := range []string{
		strings.Repeat("0", 8192),
		strings.Repeat("1", 8192),
		`"\u{` + strings.Repeat("0", 8192) + `}"`,
		`"\u{` + strings.Repeat("f", 8192) + `}"`,
		`"\u{41}"`,
		`"\u{0041}"`,
		`"\u{ABcd}"`,
		`"\u{+41}"`,
		`"\u{-41}"`,
		`"\u{0x41}"`,
		`"\u{4_1}"`,
		`"\u{G}"`,
		`"\u{100000000}"`,
	} {
		wantErrs := &errorList{}
		want := lex(src, wantErrs)
		gotErrs := &errorList{}
		got, err := lexContext(context.Background(), src, gotErrs)
		if err != nil || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gotErrs, wantErrs) {
			t.Fatalf("lex mismatch for %q: error=%v, tokens equal=%v, diagnostics equal=%v", src[:min(len(src), 20)], err, reflect.DeepEqual(got, want), reflect.DeepEqual(gotErrs, wantErrs))
		}
	}
}
