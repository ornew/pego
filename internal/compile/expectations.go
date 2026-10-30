package compile

// ExpID is an expectation (an index into descs). If MsgBit is set, it is an @error message.
type ExpID int32

// MsgBit marks an ExpID that is an @error message rather than an expectation.
const MsgBit ExpID = 1 << 30

// ExpFar is the only entry of an expectation record in a parse that does not record
// expectations. It marks that a failure happened at the position of the record and describes
// nothing.
const ExpFar ExpID = 1 << 29

// Expectations placed at the start of descs regardless of backend
const (
	IDAny ExpID = iota
	IDBeginInput
	IDEndInput
	IDBeginLine
	IDEndLine
	NumFixedDescs
)

// FixedDescs are the descriptions of the fixed expectations, in ExpID order.
var FixedDescs = []string{"any character", "beginning of input", "end of input", "beginning of line", "end of line"}
