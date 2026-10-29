package compile

// ExpID is an expectation (an index into descs). If msgBit is set, it is an @error message.
type ExpID int32

const MsgBit ExpID = 1 << 30

// Expectations placed at the start of descs regardless of backend
const (
	IDAny ExpID = iota
	IDBeginInput
	IDEndInput
	IDBeginLine
	IDEndLine
	NumFixedDescs
)

var FixedDescs = []string{"any character", "beginning of input", "end of input", "beginning of line", "end of line"}
