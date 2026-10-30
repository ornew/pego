package compile

import (
	"testing"
)

func TestStreamPragmaRejectsArguments(t *testing.T) {
	t.Parallel()
	if _, err := parseTest(`rule main = "a"* #stream(x="y")`); err == nil {
		t.Fatal("accepted arguments to #stream")
	}
}
