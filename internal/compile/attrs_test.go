package compile

import (
	"strings"
	"testing"
)

func TestRecoverTyping(t *testing.T) {
	t.Parallel()
	// The value of a recovered expression may be an error, which a declared result type must
	// admit.
	mustCompile(t, `type Stmt = { Name node[string] }
rule main = ss:stmt* $$ => $ss
rule stmt Stmt | error = s:good #recover(skip=[^;]+ ";") => $s
rule good Stmt = n:[a-z]+ #atomic ";" => Stmt{Name: $n}
`)
}

func TestAttributeErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, want string }{
		{`rule main = "a" @error`, `@error requires a message argument`},
		{`rule main = "a" @error(message=b)
rule b = "b"`, `@error: message must be a string`},
		{`rule main = "a" @nope`, `unknown attribute @nope`},
	} {
		t.Run(tc.want, func(t *testing.T) {
			if got := compileError(t, tc.src); !strings.Contains(got, tc.want) {
				t.Errorf("got %s\nwant %s", got, tc.want)
			}
		})
	}
}

// Recovery is a dedicated postfix pragma, so malformed pragma forms are
// rejected by the grammar parser before engine compilation.
func TestRecoveryPragmaSyntaxErrors(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`rule main = "a" #recover`,
		`rule main = "a" #recover(skip="x", until="y")`,
	} {
		t.Run(src, func(t *testing.T) {
			if _, err := parseTest(src); err == nil {
				t.Fatal("malformed recovery pragma parsed successfully")
			}
		})
	}
}
