package gentest

import "github.com/ornew/pego/internal/value"

// One shared grammar keeps the same scope/selection cases in the engine and generated Go/TS corpus.
const PrefixPartCuts = `type Atom = { Text string }
type Prefix = { Op node[_]; X Expr; Tag int }
type Expr ~ Atom | Prefix | error
rule main Expr = value:entry $$ => $value
rule entry Expr = case0 / case1 / case2 / case3 / case4 / case5 / case6 / case7 / case8 / case9 / case10 / case11 / case12 / case13 / case14 / case15 / case16 / case17 / case18 / case19 / case20
rule case0 Expr = "c" x:committed => $x
rule case1 Expr = "n" x:ordinary => $x
rule case2 Expr = "b" x:before => $x
rule case3 Expr = "w" x:winnerFirst => $x
rule case4 Expr = "f" x:failedFirst => $x
rule case5 Expr = "h" x:longerWinner => $x
rule case6 Expr = "s" x:shorterWinner => $x
rule case7 Expr = "t" x:ties => $x
rule case8 Expr = "k" x:winnerCut => $x
rule case9 Expr = "q" x:choiceLocal => $x
rule case10 Expr = "j" x:ruleLocal => $x
rule case11 Expr = "u" x:unicode => $x
rule case12 Expr = "v" x:trivia => $x
rule case13 Expr = "z" x:emptyCut => $x
rule case14 Expr = "o" x:emptyOrdinary => $x
rule case15 Expr = "p" x:nestedCut => $x
rule case16 Expr = "d" x:nestedOrdinary => $x
rule case17 Expr = "m" !committed x:committed => $x
rule case18 Expr = "l" !committed x:atom => $x
rule case19 Expr = "r" x:recovered => $x
rule case20 Expr = "i" x:operatorsRulePartLocal => $x

rule atom Atom = t:("~-x" / "-xy" / "¬é" / "-x" / "-z" / "-" / "a" / "é") #atomic => Atom{Text: text($t)}
rule committed Expr = #operators {
    operand atom
    level { prefix "-" #cut "z" => Prefix{Op: $op, X: $rhs, Tag: 1} }
}
rule ordinary Expr = #operators {
    operand atom
    level { prefix "-" "z" => Prefix{Op: $op, X: $rhs, Tag: 1} }
}
rule before Expr = #operators {
    operand atom
    level { prefix "-" "z" #cut "t" => Prefix{Op: $op, X: $rhs, Tag: 1} }
}
rule winnerFirst Expr = #operators {
    operand atom
    level {
        prefix "-" => Prefix{Op: $op, X: $rhs, Tag: 1}
        prefix "-" #cut "z" => Prefix{Op: $op, X: $rhs, Tag: 2}
    }
}
rule failedFirst Expr = #operators {
    operand atom
    level {
        prefix "-" #cut "z" => Prefix{Op: $op, X: $rhs, Tag: 2}
        prefix "-" => Prefix{Op: $op, X: $rhs, Tag: 1}
    }
}
rule longerWinner Expr = #operators {
    operand atom
    level {
        prefix "-" #cut "z" => Prefix{Op: $op, X: $rhs, Tag: 2}
        prefix "-x" => Prefix{Op: $op, X: $rhs, Tag: 1}
    }
}
rule shorterWinner Expr = #operators {
    operand atom
    level {
        prefix "--" #cut "z" => Prefix{Op: $op, X: $rhs, Tag: 2}
        prefix "-" => Prefix{Op: $op, X: $rhs, Tag: 1}
    }
}
rule ties Expr = #operators {
    operand atom
    level {
        prefix "-" => Prefix{Op: $op, X: $rhs, Tag: 1}
        prefix "-" => Prefix{Op: $op, X: $rhs, Tag: 2}
        prefix "-" #cut "z" => Prefix{Op: $op, X: $rhs, Tag: 3}
    }
}
rule winnerCut Expr = #operators {
    operand atom
    level {
        prefix "-" #cut => Prefix{Op: $op, X: $rhs, Tag: 1}
        prefix "-" #cut "z" => Prefix{Op: $op, X: $rhs, Tag: 2}
    }
}
rule choiceLocal Expr = #operators {
    operand atom
    level { prefix ("-" #cut "z" / "-") "q" => Prefix{Op: $op, X: $rhs, Tag: 1} }
}
rule ruleLocal Expr = #operators {
    operand atom
    level { prefix part => Prefix{Op: $op, X: $rhs, Tag: 1} }
}
rule part = "-" #cut "z"
rule unicode Expr = #operators {
    operand atom
    level { prefix "¬" #cut "z" => Prefix{Op: $op, X: $rhs, Tag: 1} }
}
rule trivia Expr = #operators {
    skip " "*
    operand atom
    level { prefix "-" #cut "z" => Prefix{Op: $op, X: $rhs, Tag: 1} }
}
rule emptyCut Expr = #operators {
    operand atom
    level { prefix _ #cut => Prefix{Op: $op, X: $rhs, Tag: 1} }
}
rule emptyOrdinary Expr = #operators {
    operand atom
    level { prefix _ => Prefix{Op: $op, X: $rhs, Tag: 1} }
}
rule nestedCut Expr = #operators {
    operand atom
    level {
        prefix "~" #cut => Prefix{Op: $op, X: $rhs, Tag: 1}
        prefix "-" #cut "z" => Prefix{Op: $op, X: $rhs, Tag: 2}
    }
}
rule nestedOrdinary Expr = #operators {
    operand atom
    level {
        prefix "~" => Prefix{Op: $op, X: $rhs, Tag: 1}
        prefix "-" #cut "z" => Prefix{Op: $op, X: $rhs, Tag: 2}
    }
}
rule recovered Expr = committed #recover(skip=.*)
rule operatorsRulePartLocal Expr = #operators {
    operand atom
    level { prefix committed #atomic => Prefix{Op: $op, X: $rhs, Tag: 1} }
}
`

var PrefixPartCutInputs = []struct {
	Input string
	Kind  string // Empty means unrecovered failure; Error means successful recovery.
}{
	{"c-x", ""}, {"c-", ""}, {"c-za", "Prefix"}, {"ca", "Atom"},
	{"n-x", "Atom"}, {"n-", "Atom"}, {"n-za", "Prefix"},
	{"b-x", "Atom"}, {"b-z", ""}, {"b-zta", "Prefix"},
	{"w-a", "Prefix"}, {"w-", "Atom"}, {"w-x", "Atom"},
	{"f-a", "Prefix"}, {"f-", "Atom"}, {"f-x", "Atom"},
	{"h-xa", "Prefix"}, {"h-x", "Atom"}, {"s--a", "Prefix"},
	{"t-a", "Prefix"}, {"k-a", "Prefix"}, {"k-", ""}, {"k-x", ""},
	{"q-x", "Atom"}, {"j-x", "Atom"},
	{"u¬é", ""}, {"u¬zé", "Prefix"},
	{"v -x", ""}, {"v a", "Atom"}, {"v -z a", "Prefix"},
	{"za", ""}, {"oa", "Atom"},
	{"p~-x", ""}, {"p~a", "Prefix"}, {"d~-x", "Atom"},
	{"m-x", ""}, {"l-x", "Atom"}, {"r-x", value.TypeError}, {"r-za", "Prefix"}, {"i-x", "Atom"}, {"iaa", "Prefix"},
}

func PrefixPartCutCorpus() Case {
	c := Case{Name: "OperatorsRule prefix-part cuts", Src: PrefixPartCuts}
	for _, in := range PrefixPartCutInputs {
		c.Inputs = append(c.Inputs, in.Input)
	}
	return c
}
