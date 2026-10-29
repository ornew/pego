package gentest

// Arith is a grammar with an Operators Rule expression that builds a CST (no actions).
const Arith = `rule main = e $$
rule e = #operators {
    skip " "*
    operand [0-9]+ #atomic / "(" x:e ")"
    level { infix none "==" }
    level { infix left "+" / "-" }
    level { infix left "*" / "/" }
    level { prefix "-" }
    level { infix right "^" }
    level { postfix "!" }
}`

const AST = `type Num = { Text string }
type Bin = { L Expr; Op node[string]; R Expr }
type Neg = { X Expr }
type Cond = { C Expr; T Expr; F Expr }
type Call = { Fn Expr; Args []Expr }
type Index = { X Expr; I Expr }
type Expr ~ Num | Bin | Neg | Cond | Call | Index | node[string]

rule main = x:e $$ => $x
rule num Num = t:([0-9]+) #atomic => Num{Text: text($t)}
rule ident = [a-z]+ #atomic
rule args = (
    first:level(e, assignment) rest:(" "* "," r:level(e, assignment))*
) => concat(list($first), map($rest, (x) => x.$r))
rule e Expr = #operators {
    skip " "*
    operand num
    operand ident
    operand "(" x:e ")" => $x
    level { infix left "," => Bin{L: $lhs, Op: $op, R: $rhs} }
    level assignment { infix right "=" => Bin{L: $lhs, Op: $op, R: $rhs} }
    level { infix right "?" t:e " "* ":" => Cond{C: $lhs, T: $t, F: $rhs} }
    level { infix right "->" => Bin{L: $lhs, Op: $op, R: $rhs} }
    level { infix left "+" / "-" => Bin{L: $lhs, Op: $op, R: $rhs} }
    level { prefix "-" => Neg{X: $rhs} }
    level {
        postfix "(" args:args? " "* ")" => Call{Fn: $lhs, Args: concat($args)}
        postfix "[" i:e "]" => Index{X: $lhs, I: $i}
    }
}`

const RestrictedPrefix = `type Atom = { Text string }
type Prefix = { Op node[string]; X Expr }
type Binary = { L Expr; Op node[string]; R Expr }
type Postfix = { X Expr; Op node[string] }
type Expr ~ Atom | Prefix | Binary | Postfix

rule main = x:level(e, mul) $$ => $x
rule atom Atom = t:("é" / "x" / "y" / "z") #atomic => Atom{Text: text($t)}
rule e Expr = #operators {
    operand atom
    level loose {
        prefix "~" => Prefix{Op: $op, X: $rhs}
        infix left "|" => Binary{L: $lhs, Op: $op, R: $rhs}
        postfix "?" => Postfix{X: $lhs, Op: $op}
    }
    level add { infix left "+" => Binary{L: $lhs, Op: $op, R: $rhs} }
    level mul { infix left "*" => Binary{L: $lhs, Op: $op, R: $rhs} }
    level power {
        infix right "^" => Binary{L: $lhs, Op: $op, R: $rhs}
        postfix "!" => Postfix{X: $lhs, Op: $op}
    }
}`
