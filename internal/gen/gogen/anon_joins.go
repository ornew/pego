package gogen

import (
	"strings"

	"github.com/ornew/zeroparse/internal/compile"
)

// anonJoinName returns the Go name of the interface of an undeclared join: its members in the order
// written, joined by Or; one declared node type and error is that type and Error.
func anonJoinName(u compile.JoinTy) string {
	parts := make([]string, len(u.Alts))
	for i, a := range u.Alts {
		switch a := a.(type) {
		case compile.NamedTy:
			parts[i] = a.Name
		case compile.NodeTy:
			parts[i] = a.Name
		}
	}
	if len(parts) == 1 {
		parts = append(parts, "Error")
	}
	return strings.Join(parts, "Or")
}

// anonJoinText returns the join u of anonJoins as written: one declared node type is that type and
// error.
func anonJoinText(u compile.JoinTy) string {
	if len(u.Alts) == 1 {
		return u.String() + " | error"
	}
	return u.String()
}
