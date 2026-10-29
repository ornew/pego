package gentest

import (
	"fmt"
	"strconv"
)

func WideRepetitionCases() []Case {
	if strconv.IntSize < 64 {
		return nil // Source bounds follow the implementation's int range.
	}
	var cases []Case
	for _, bound := range []string{"2147483648", "4294967296", "9223372036854775807"} {
		for _, c := range []struct{ Name, Body string }{
			{"minimum", `"é" #repeat(%s) $$`},
			{"unbounded minimum", `"é" #repeat(%s,) $$`},
			{"scan minimum", `[é] #repeat(%s) #atomic $$`},
			{"maximum", `"é" #repeat(0,%s) $$`},
			{"class scan", `[é] #repeat(0,%s) #atomic $$`},
			{"any scan", `. #repeat(0,%s) #atomic $$`},
			{"projection", `xs:(v:"é") #repeat(0,%s) $$ => map($xs, (x) => x.$v)`},
		} {
			cases = append(cases, Case{"wide " + c.Name + " " + bound, "rule main = " + fmt.Sprintf(c.Body, bound), []string{"", "é", "éé", "x"}})
		}
	}
	return cases
}
