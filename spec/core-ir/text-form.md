# Text Form

The engine's printer writes a module as text: one line per rule and per node, with the children of a node indented by two
spaces under it. This chapter explains how to read that text. It is a readable form, not a format to parse, and it may
change before the first release. With attributes, each node line ends with the node's attributes in brackets.

## Rule Lines

A rule line is `rule`, the rule's ID and name, then the following that apply, in this order: `want`; the policy flags
`memo`, `transient`, `plain`, `leader`, `splice` and `novalue`; `seen=n`; `vars=[...]`; `scope=[names]`; `twin=id`;
the finish, which is `=> term` for an action, `splice`, or `scope-value` with the shape; and `as Type` if the rule's
value is converted to a node type. The rule's body follows, indented one level.

## Node Lines

| Node | Printed as |
|:--|:--|
| `Lit` | `lit "text"`, or `lit "text"i` if case-insensitive |
| `ClassNode` | `class [a-z]`: ranges, with `^` after the bracket if negated. A code point that is not a printable ASCII character, or is `-`, `]`, `\` or `^`, is written `\uXXXX` |
| `Any`, `Cut`, `Succeed`, `FailNode`, `Seq`, `Opt`, `Text`, `Drop`, `Recover`, `Missing`, `Stream`, `Terminal` | `any`, `cut`, `succeed`, `fail`, `seq`, `opt`, `text`, `drop`, `recover`, `missing`, `stream`, `terminal` |
| `Anchor` | `anchor begin-input`, `end-input`, `begin-line` or `end-line` |
| `Choice` | `choice`, with each alternative as an `alt` line, followed by ` guard=` and the first terminal, and ` class` for a guarded class |
| `Repeat` | `repeat min max`, then ` scope=[names]` and ` project=name` if present |
| `Look` | `and` or, if negative, `not` |
| `Call` | `call id:name`, then ` level=n` if not 0 |
| `Nest` | `nest id:name` |
| `OperatorsRule` | `operators`, then its lines: `skip`, `operand` and `op id kind assoc level=n`, each with `scope=[names]`, the shape, ` => action` if it has one, and its body below |
| `Capture` | `capture slot name`, then ` project=field` if present |
| `Pred`, `BindNode` | `pred term`, `bind name = term` |
| `Label` | `error "message"` or `diagnostic "message"` |
| `Scan` | `scan class min max`, or `scan any min max` |
| `ClassRun` | `classrun` and its classes |

A first terminal is a class, or a quoted code point, with `i` after it if case-insensitive, and `+n` for a depth n.
The shape is written `(elems,k=n,mode=n,tuple,shared)`, with the parts that do not apply left out.

## Attributes

With attributes, a node line ends with `[` and the following that apply, separated by spaces, and `]`: `want` and the
shape; `nullable`; `first=` and the first terminal; and the effects, separated by commas, named `consume`, `fail`,
`expect`, `capture[slots]`, `bind`, `recover`, `cut`, `raise`, `call` and `commit`.

## Terms

A term is printed as follows: a string constant quoted; `$slot:name` for a slot; `param:name`, `$lhs`, `$rhs` and `$op`
for locals; `var:name`; `x.Name` for a member; `Type{Field: term, ...}` for a struct, with `!` after the closing brace
for the final struct of a rule or line; `Node(term)` for a conversion; `(a, b)` for a tuple; `f(args)` for a built-in;
`fn(params) body` for a lambda; `(a op b)` and `opx` for operations; `text-eq(...)`, `field-cmp(...)`, `text-in(...)`,
`concat-fused(...)` and `projected-map(...)` for the rewritten forms.

## Example

The example grammar is the following.

```pego
package list

type Item = { Name node[string] }

@start
rule main = items:item+ $$

rule item Item = ws n:name ("," / ";")? => Item{Name: $n}

rule name node[string] = [a-z]+ #atomic

rule ws = [ \t]*
```

The module as built from the compiled grammar, printed without attributes:

```text
rule 0 main want memo transient scope=[items] scope-value(elems,k=1,mode=2,tuple)
  seq
    capture 0 items
      repeat 1 -1
        call 1:item
    anchor end-input
rule 1 item memo transient scope=[n] => Item{Name: $0:n}!
  seq
    call 4:ws
    capture 0 n
      call 2:name
    opt
      choice
        alt
          lit ","
        alt
          lit ";"
rule 2 name want memo transient plain scope-value(k=1,mode=1)
  text
    repeat 1 -1
      class [a-z]
rule 3 ws want memo transient plain twin=4 scope-value(k=1,mode=1)
  repeat 0 -1
    class [\u0020\u0009]
rule 4 ws memo transient plain novalue
  repeat 0 -1
    class [\u0020\u0009]
```

Rule 1 calls rule 4, the value-free twin of `ws`, because it wants no value there. The action's
`$0:n` is slot 0, named `n`; the final `!` marks the term that makes the rule's own value.

The same module after guards and scans were introduced and analysis was run, printed with attributes (rules 0 to 2):

```text
rule 0 main want memo transient scope=[items] scope-value(elems,k=1,mode=2,tuple)
  seq [want(elems,k=1,mode=2,tuple) consume,fail,expect,capture[0],raise,call]
    capture 0 items [want(k=1,mode=1,tuple) consume,fail,expect,capture[0],raise,call]
      repeat 1 -1 [want(k=1,mode=1) consume,fail,expect,raise,call]
        call 1:item [want(k=1,mode=1) consume,fail,expect,raise,call]
    anchor end-input [want(k=0,mode=0,tuple) nullable fail,expect]
rule 1 item memo transient scope=[n] => Item{Name: $0:n}!
  seq [consume,fail,expect,capture[0],raise,call]
    call 4:ws [nullable consume,expect,raise,call]
    capture 0 n [consume,fail,expect,capture[0],raise,call]
      call 2:name [want(k=1,mode=1) consume,fail,expect,raise,call]
    opt [nullable consume,expect]
      choice [consume,fail,expect]
        alt guard=','
          lit "," [first=',' consume,fail,expect]
        alt guard=';'
          lit ";" [first=';' consume,fail,expect]
rule 2 name want memo transient plain scope-value(k=1,mode=1)
  text [want(k=1,mode=1) consume,fail,expect]
    scan [a-z] 1 -1 [consume,fail,expect]
```

`name` shows a `Scan` in place of the value-free repetition under `#atomic`; `item` shows the guards of the two
alternatives. The twin `ws` (rule 4) becomes `scan [\u0020\u0009] 0 -1`.

