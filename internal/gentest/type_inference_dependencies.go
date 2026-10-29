package gentest

import (
	"fmt"
	"strings"
)

func InferredChain(depth int, reverse bool, mainType string) string {
	var src strings.Builder
	src.WriteString("type P = {}\n")
	annotation := ""
	if mainType != "" {
		annotation = " " + strings.TrimPrefix(mainType, ":")
	}
	fmt.Fprintf(&src, "rule main%s = r0\n", annotation)
	for j := range depth {
		i := j
		if reverse {
			i = depth - 1 - j
		}
		if i == depth-1 {
			fmt.Fprintf(&src, "rule r%d = \"a\" => P{}\n", i)
		} else {
			fmt.Fprintf(&src, "rule r%d = r%d\n", i, i+1)
		}
	}
	return src.String()
}
