package gentest

const LeftRecursionDeadTail = `rule r1 = r2?
rule r2 = (("a" / r1 / "\n") / ("" / "\n") / (r2 / $$)) #repeat(0, 1)`

var ReachabilityGeneratedCases = []Case{
	{"unreachable LR tail", LeftRecursionDeadTail + "\nrule main = r2", []string{"", "a", "\n", "aa", "b"}},
	{"unreachable LR alias tail", `rule r1 = r2?
rule r2 = (("a" / r1 / "\n") / empty / (r2 / $$)) #repeat(0, 1)
rule empty = alias
rule alias = ""
rule main = r2`, []string{"", "a", "\n", "b"}},
	{"LR cut inside optional", `rule r1 = r2?
rule r2 = (("a" / r1 / "\n") / ("x" #cut "y")? / (r2 / $$)) #repeat(0, 1)
rule main = r2`, []string{"", "x", "xy", "\n"}},
	{"left recursion in recovery skip", `rule main = a
rule a = "x" #recover(skip=a) / "y"`, []string{"", "y", "x", "b"}},
	{"unreachable recovery chain", `rule r1 = r2?
rule r2 = (r1 / main / r2) #repeat(0, 1)
rule main = empty #recover(skip=main)
rule empty = ""`, []string{"", "a", "\n"}},
	{"cut in recovery skip", `rule main = _|_ #recover(skip=#cut "x")? / ""`, []string{"", "x", "b"}},
}
