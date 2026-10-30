package gentest

// IgnoreCaseGenGrammar has case-insensitive literals in a choice, whose alternatives the generated
// parsers dispatch on their first character, and in a sequence.
const IgnoreCaseGenGrammar = `rule main = ("select"i / "from"i / "Sel"i "ect"i) " " "xy"i`

// IgnoreCaseGenInputs are parsed by every backend. They include a non-ASCII letter and the Kelvin
// sign, which the folding must not match.
var IgnoreCaseGenInputs = []string{
	"SELECT xY", "from XY", "selEct xy", "SEL ect xy", "select", "fRoM xy", "SELECT xz",
	"SELECX xy", "selecté xy", "SeLeCt xy", "KEL", "", "Sel ect XY", "FROM xy",
}
