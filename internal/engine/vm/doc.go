// Package vm is the bytecode backend of the engine: it runs a compiled module with one of two
// drivers over the same instruction loop. New prepares a module for the recursive driver, which
// recurses on the host stack for each rule call; NewIterative prepares it for the iterative
// driver, which runs rule calls, left-recursion growth, Operators Rule loops and longest matches
// as frames on a stack of its own. Both return a Program whose rules the parser of package core
// runs; Machine is the backend state that a parse keeps beside the parser.
//
// NewMachine, Machine.Values and Machine.Parser are exported for the tests of package document,
// which check how a Document keeps the VM state between parses; the engine itself needs only
// New, NewIterative and Program.
//
// The VM shares the runtime of core with the closure backend, which is the reference execution
// path, and must agree with it.
package vm
