package gentest

import "strings"

// CountBodies returns the number of rule bodies that the generated parser source prints.
func CountBodies(src []byte) int { return strings.Count(string(src), "].Body = ") }
