package syntax

import (
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestBlockCommentsNestedRanges(t *testing.T) {
	src := "/*first\r\n/*inner*/\r\nlast*/ x // trailing  \r\ny"
	ts := Tokenize(src)
	if len(ts) != 4 {
		t.Fatalf("tokens: %+v", ts)
	}
	if ts[0].Kind != TokenComment || ts[0].Text != "/*first\r\n/*inner*/\r\nlast*/" || ts[0].Pos != (grammar.Pos{Line: 1, Col: 1}) || ts[0].End != (grammar.Pos{Line: 3, Col: 7}) {
		t.Fatalf("nested comment: %+v", ts[0])
	}
	if ts[1].Text != "x" || ts[1].Pos != (grammar.Pos{Line: 3, Col: 8}) || ts[2].Text != "// trailing" || ts[2].End != (grammar.Pos{Line: 3, Col: 21}) || ts[3].Pos != (grammar.Pos{Line: 4, Col: 1}) {
		t.Fatalf("trailing positions: %+v", ts)
	}
}

func TestBlockCommentsLiteralShielding(t *testing.T) {
	err := &errorList{}
	ts := lex("\"/*not a comment*/\" [/*a*/] /*outer /*inner*/ end*/ z", err)
	if len(err.list) != 0 {
		t.Fatal(err.list)
	}
	if len(ts) != 4 || ts[0].kind != tString || ts[1].kind != tCharClass || ts[2].text != "z" || len(ts[2].comments)+btoi(ts[2].lineComment != nil) != 1 {
		t.Fatalf("tokens: %+v", ts)
	}
}

func TestBlockCommentsUnterminated(t *testing.T) {
	err := &errorList{}
	lex("a /*outer\n/*inner*/", err)
	if len(err.list) != 1 || err.list[0].Msg != "unterminated block comment" || err.list[0].Pos != (grammar.Pos{Line: 1, Col: 3}) || err.list[0].End != (grammar.Pos{Line: 2, Col: 10}) {
		t.Fatalf("errors: %+v", err.list)
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
