package syntax

import (
	"strings"
	"testing"
)

func TestPragmaAdjacency(t *testing.T) {
	for _, pragma := range []string{"atomic", "stream", "repeat(2)", `recover(skip=";")`} {
		for _, gap := range []string{" ", "\t", "/* gap */", "\n"} {
			_, err := Parse("rule main = (\"x\" #" + gap + pragma + ")")
			if err == nil || !strings.Contains(err.Error(), "pragma name must immediately follow") {
				t.Fatalf("gap %q before %s: %v", gap, pragma, err)
			}
		}
	}
	for _, pragma := range []string{"repeat", "recover"} {
		args := "(2)"
		if pragma == "recover" {
			args = `(skip=";")`
		}
		for _, gap := range []string{" ", "\t", "/* gap */", "\n"} {
			_, err := Parse("rule main = (\"x\" #" + pragma + gap + args + ")")
			if err == nil || !strings.Contains(err.Error(), "pragma arguments must immediately follow") {
				t.Fatalf("gap %q after %s: %v", gap, pragma, err)
			}
		}
	}
	for _, source := range []string{
		`rule main = "x" #atomic()`, `rule main = "x" #stream()`,
		`rule main = "x" #repeat( /* inside */ 2 )`,
		`rule main = "x" #recover( /* inside */ skip = ";" )`,
		`rule main = "x" #atomic ("y")`,
		`rule main = "x" #stream ("y")`,
	} {
		if _, err := Parse(source); err != nil {
			t.Fatalf("valid source %q: %v", source, err)
		}
	}
}
