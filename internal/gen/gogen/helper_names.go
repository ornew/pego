package gogen

// ordinaryHelperName reports whether the runtime has an Ordinary form of the method name, which
// a parser without budget controls and Documents calls instead (the runtime writes them by hand).
func ordinaryHelperName(name string) bool {
	switch name {
	case "NewNode", "NewTerminal", "NewBranch", "NewMatch", "SetCapture", "Expect", "Peek", "MatchLiteral", "MatchLiteralFold", "MatchAny",
		"AtEnd", "AtLineStart", "AtEndBatch", "AtLineStartBatch", "AtLineEnd", "SpendRead":
		return true
	}
	return false
}
