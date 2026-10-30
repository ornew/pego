package gentest

// StartEntryNames are the names the start-rule entry tests try: two start rules, a rule that is
// not one, a hidden rule, an undefined rule and no name.
var StartEntryNames = []string{"main", "item", "other", "_h", "nope", ""}

// StartRestrictionSource is the grammar of the start-rule entry tests: two start rules, a rule that is
// not one and a hidden rule.
const StartRestrictionSource = `@start(default)
rule main = item* #stream $$
@start
rule item = "a"
rule other = "b" _h?
rule _h = "c"
`
