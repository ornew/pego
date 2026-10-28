package engine

// expID is an expectation (an index into descs). If msgBit is set, it is an @error message.
type expID int32

const msgBit expID = 1 << 30

// Expectations placed at the start of descs regardless of backend
const (
	idAny expID = iota
	idBeginInput
	idEndInput
	idBeginLine
	idEndLine
	numFixedDescs
)

var fixedDescs = []string{"any character", "beginning of input", "end of input", "beginning of line", "end of line"}
