package core

import "github.com/ornew/pego/internal/compile"

// staticProgram and staticRule are the compiler's program and rule. SharedProgram and Rule embed
// them under these names, so the embedded fields stay unexported.
type (
	staticProgram = compile.Program
	staticRule    = compile.Rule
)

// ExpID and MsgBit are the compiler's expectation IDs and the bit that marks an @error message;
// the runtime records them under these names, as the runtime package of generated parsers does.
type ExpID = compile.ExpID

const MsgBit = compile.MsgBit
