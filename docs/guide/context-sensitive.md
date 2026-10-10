# Context-Sensitive Parsing

A plain PEG describes what may follow what, but it cannot remember anything: it cannot check that a closing tag has the
name of its opening tag, that a block is indented like its siblings, or that a here-document ends with the delimiter
that started it. PEGO adds three tools for this: **captures that predicates can read**, **variables** that carry context
down into the rules a rule calls, and **lookahead** that can look at the input without consuming it. This guide shows
how they work and how to combine them, with complete grammars for matching tags, indentation-based blocks,
here-documents and similar constructs.

It complements the normative text in [Predicates and Variables](../../spec/predicates.md), [Parsing
Expressions](../../spec/parser-expressions.md) (captures, lookahead) and [Attributes](../../spec/attributes.md). For
error messages and recovery, see the [error guide](errors-and-recovery.md).

Contents:

1. [The toolbox](#1-the-toolbox): predicates, variables, lookahead, attributes
2. [Cost: memoization and variables](#2-cost-memoization-and-variables)
3. [Recipes](#3-recipes): [matching tags](#matching-tags), [indentation blocks](#indentation-blocks),
   [newlines that depend on context](#newlines-that-depend-on-context), [here-documents](#here-documents),
   [long brackets](#long-brackets), [limiting nesting](#limiting-nesting)
4. [Pitfalls](#4-pitfalls)

The examples use the `pego` command (`go build -o pego ./cmd/pego`). Commands are followed by their output, standard
output and standard error together. A grammar block whose first line is `// name.pego` is a complete file to save under
that name; a block marked `(changed rules)` replaces the rules of the same names in that file. Every command and its
output below were run as shown, in this order.

## 1. The toolbox

### Predicates

A predicate `[expr]` evaluates an expression where it stands in a sequence. It **fails if the result is `false` or the
evaluation is an error, and succeeds otherwise**. It consumes no input and has no value, like a lookahead whose test is
computed instead of matched.

The expression is the same language as an [action](../../spec/actions.md): operators, `len`, `text`, and so on. It can
read the captures `$label` made **earlier in the same rule** (positional references such as `$1` are not allowed in
predicates). Three consequences are worth remembering.

**Only `false` is false.** A predicate that evaluates to the integer `0` succeeds. Always write the comparison:

```pego
// truthy.pego
def zero = [0] "a"
def lenonly = s:@"x"* [len($s)] "a"
def compared = s:@"x"* [len($s) > 0] "a"
```

```bash
pego parse -g truthy.pego -s lenonly -f sexpr -i 'a'
```

```text
(Seq "" "a" s="")@lenonly
```

`[len($s)]` is the integer 0 here, and the rule matches anyway. The comparison fails as intended:

```bash
pego parse -g truthy.pego -s compared -f sexpr -i 'a'
```

```text
pego: 1:1: syntax error: expected "x"
```

**Compare text, not nodes.** `==` on nodes compares identity, and two captures are different nodes even when they
cover the same text. Use `text(x)` (the matched text) or `len(x)`:

```pego
// identity.pego
def by_identity = "<" a:name "></" b:name [$a == $b] ">"
def by_text = "<" a:name "></" b:name [text($a) == text($b)] ">"
def name = @((?a-z)+)
```

```bash
pego parse -g identity.pego -s by_identity -i '<a></a>'
```

```text
pego: 1:7: syntax error: expected (?a-z)
```

```bash
pego parse -g identity.pego -s by_text -f sexpr -i '<a></a>'
```

```text
(Seq "<" "a"@name "></" "a"@name ">" a="a"@name b="a"@name)@by_text
```

The first command shows the third consequence: **a failed predicate says nothing**. The farthest-failure rule of the
[error guide](errors-and-recovery.md#what-is-not-in-the-list) records no expected item for it, so the message points at
whatever else failed furthest (here, the name rule looking for one more letter). Put `#error(message="...")` on the
predicate, as in the recipes below.

Also remember that a mistake inside a predicate is not reported either: an evaluation error just fails the predicate. A
misspelled variable name is such an error, and the type checker does not catch it (variables are defined dynamically by
callers):

```pego
// typo.pego
def main = [indent = 0] "a" [idnent == 0]
```

```bash
pego parse -g typo.pego -i 'a'
```

```text
pego: 1:1: syntax error
```

If a predicate unexpectedly fails, check the names first.

### Variables

A predicate `[name = value]` **defines** a variable, and `[name]` or any expression that mentions `name` **reads** it.
Values are `int`, `string` or `bool`. Definitions never modify anything: a variable is an immutable binding in an
environment that flows **down** the call tree.

| Rule | Meaning |
|:--|:--|
| Each rule invocation has its own scope | A rule can read the variables of all the rules that called it, directly or indirectly |
| A new definition shadows | Defining `x` where `x` exists creates a new binding; the old one is untouched |
| A callee's definitions are its own | Shadowing or defining in a called rule does not affect the caller |
| Backtracking undoes definitions | Returning to an earlier point in the input forgets what was defined after it |
| Reading an undefined variable fails the predicate | It is an evaluation error |

The following grammar has one start rule for each row. All the rules are pure predicates, so the commands use `-check`,
which prints `ok` on success:

```pego
// lab.pego
// A callee reads the caller's variable.
def inherit = [x = 1] reads_x
def reads_x = [x == 1]

// A callee's definition shadows the caller's only inside the callee.
def shadow = [x = 1] shadows_x [x == 1]
def shadows_x = [x == 1] [x = 2] [x == 2]

// A definition made in a callee is not visible to the caller.
def hidden = defines_y [y == 1]
def defines_y = [y = 1]

// Backtracking undoes definitions: the first alternative defined x = 1, then failed.
def undone = ([x = 1] "a" "b" / [x = 2] "a" "c") [x == 2]

// A definition made inside a repetition, an optional or a positive lookahead stays.
def stays = ([r = 1] "a")* [r == 1]
def kept = ([o = 1] "a")? [o == 1]
def peeked = &([p = 1]) [p == 1]
```

```bash
pego parse -g lab.pego -s inherit -check -i ''
```

```text
ok
```

```bash
pego parse -g lab.pego -s shadow -check -i ''
```

```text
ok
```

```bash
pego parse -g lab.pego -s hidden -check -i ''
```

```text
pego: 1:1: syntax error
```

```bash
pego parse -g lab.pego -s undone -check -i 'ac'
```

```text
ok
```

```bash
pego parse -g lab.pego -s stays -check -i 'aa'
```

```text
ok
```

```bash
pego parse -g lab.pego -s kept -check -i 'a'
```

```text
ok
```

```bash
pego parse -g lab.pego -s peeked -check -i ''
```

```text
ok
```

Think of the environment as a stack of frames that exists only while the rules are running: a rule sees what its callers
defined, pushes its own definitions on top, and everything it defined disappears when it returns. Two practical
consequences:

- **Context flows downward only.** A variable cannot carry information from a callee back to its caller or from one
  sibling to the next, because the definition is gone when the callee returns. Information that has to flow the other
  way belongs in the tree: capture it and build it in an action.
- **A counter is a chain of shadows.** `[depth = depth + 1]` does not increment anything; it defines a new `depth` in
  this invocation, computed from the caller's. Siblings each start from the same parent value (see [limiting
  nesting](#limiting-nesting)).

A definition persists for the rest of the rule it is made in, even when it was written inside a repetition, an optional
expression or a positive lookahead (the last three rows above). Definitions in a branch that is abandoned are undone.

Variables are meant for predicates. (The engine also lets an action read a variable, but there a missing variable aborts
the whole parse with a run-time error instead of failing a predicate; this guide does not use it.)

### Lookahead

`&a` succeeds if `a` matches here, and `!a` if it does not; neither consumes input or has a value. In context-sensitive
grammars, lookahead is how a rule **looks at the input before deciding**.

A failure inside a lookahead is not reported as an expected item. This is what makes `!"if"` a clean way to exclude a
keyword, and `(&(? \t) .)*` a whitespace rule that never clutters error messages.

**Captures and variable definitions made inside a positive lookahead stay in effect after it; only the input position
is restored.** This lets a rule peek at something, bind it, and test it before consuming:

```pego
// peek.pego
def main = &(s:(?a-z)+) [len($s) == 3] (?a-z)+ $$
```

```bash
pego parse -g peek.pego -check -i 'abc'
```

```text
ok
```

```bash
pego parse -g peek.pego -check -i 'abcd'
```

```text
pego: 1:1: syntax error
```

The indentation recipe below uses exactly this to read the indentation of the *next* line without consuming it.

A negative lookahead must not contain captures (the grammar is rejected). To test "is not equal to the saved value",
use a predicate with `!=`, as the here-document recipe does, or call a rule that has its own captures from the `!`, as
the long-bracket recipe does.

### Attributes

Three attributes matter here:

- `#error(message="...")` on a predicate (or on the expression around it) gives a failed check a message. Without it,
  the error points at the wrong place or says nothing (see above). The recipes use it every time.
- `#recover(skip=e)` restores the variables to the values they had where the expression began, so definitions made
  inside the failed expression are gone after the recovery. The `skip` expression is matched in that restored context,
  so it can read variables: a `skip` can skip the rest of a broken line **and the more deeply indented lines
  after it** by comparing indentation ([see the error guide](errors-and-recovery.md#skip-to-the-next-statement-or-line-and-continue)).
- `#stream` repetitions see the variables defined before them: with `def main = [mode = 1] rs:rec* #stream $$` and
  `def rec = [mode == 1] ...`, every streamed element reads `mode`.

### Choosing a tool

| You need to ... | Use |
|:--|:--|
| compare two pieces of the input inside one rule | captures and a predicate on their text (`[text($a) == text($b)]`) |
| make a fact about the input so far available to the rules called later | a variable (`[name = value]`) |
| decide by what follows, without consuming it | lookahead; a positive one keeps the captures and variables it made |
| say why a check failed | `#error` on the predicate |
| skip bad input and carry on | `#recover`, whose `skip` can read variables |

## 2. Cost: memoization and variables

A packrat parser caches the result of a rule at each input position, so that backtracking does not repeat work. A rule
that reads variables does not depend on the position alone: the same rule at the same position can succeed with one
`indent` and fail with another. PEGO therefore keys the cache by the position **and the values of the variables the rule
can read** in predicates and actions, including Pratt actions, directly or through the rules it calls (see the
[spec](../../spec/predicates.md#interaction-with-memoization) and [optimization 012](../optimizations/012-memoizing-rules-that-read-variables.md)). A cached result is reused only when
those values are equal, so memoization never changes what a parse returns, only what it costs.

What this means when you write a grammar:

- **Write it naturally.** Context-sensitive grammars used to be exponential in the worst case, because a rule that reads
  a variable was not memoized at all (optimization 012 describes an early Python grammar being written in a
  parse-once-then-fold style to avoid that). Now an ordered choice that re-parses a prefix is cached like any other:

```pego
// memo.pego
// Each level parses the nested group, fails at "x", and parses it again for "y". Without
// memoization that is 2^n work for n levels; `group` reads `mode`.
def main = [mode = 1] group $$
def group = "(" group ")" "x" [mode == 1] / "(" group ")" "y" [mode == 1] / "z" [mode == 1]
```

```bash
pego parse -g memo.pego -check -i "$(printf '(%.0s' {1..40})z$(printf ')y%.0s' {1..40})"
```

```text
ok
```

- **Only variables a rule can read split its cache entries.** A variable that no predicate below a rule reads costs that
  rule nothing.
- **Keep the values few.** Each distinct value is a separate cache entry. An indentation width, a bool, a tag name or a
  nesting level take few values; a variable that changes at almost every call (a position, a growing string) gives every
  call its own entry, so a rule that reads it is never reused. If a value is needed only to build the tree, capture it
  and use an action instead of a variable.

Each current environment stores one value per distinct variable name. Repeated assignments do not lengthen lookup
paths; backtracking and rule returns still restore the earlier values. Assigning the same scalar value reuses the
environment. Changing a value behind other names copies that prefix to preserve saved environments, so frequent
changes to many distinct names can allocate more than a grammar with one variable. See
[optimization 069](../optimizations/069-bound-persistent-variable-binding-histories.md) for measurements.

## 3. Recipes

### Matching tags

The closing tag must repeat the name of the opening tag. When both are in the same rule, capture the names and compare
the text. That is how [parsers/xml](../../parsers/xml/xml.pego) does it (a rule `element`, with
`"<" n:name ... "</" e:name [text($e) == text($n)] #error(message="mismatched end tag")`):

```bash
pego parse -g parsers/xml/xml.pego -f sexpr -i '<a><b>text</c></a>'
```

```text
pego: 1:14: mismatched end tag
```

(Run from the repository root.) When the closing tag is parsed by **another rule**, because it is shared or because the
content rule is complicated, the name must travel to it: define a variable after the opening tag. The nesting works by
itself, because each `element` invocation defines its own `tag` and the definition disappears when it returns:

```pego
// tags.pego
package tags

type Name terminal
type Chars terminal
type Element struct { Name Name, Children []Content }
type Content = Element | Chars

def main: Element = e:element $$ -> $e

// The open tag defines `tag`; every rule called after that can read it.
def element: Element = "<" n:name ">" [tag = text($n)] cs:content* close_tag
    -> new Element{Name: $n, Children: $cs}
def content: Content = element / chars
def chars: Chars = (?^<)+

// The close tag is parsed by another rule: it finds the name in `tag`.
def close_tag = "</" n:name [text($n) == tag] #error(message="mismatched end tag") ">"

def name: Name = (?a-z)+
```

```bash
pego parse -g tags.pego -f sexpr -i '<a><b>x</b><c>y</c></a>'
```

```text
(Element Children=[(Element Children=[Chars"x"@chars] Name=Name"b"@name) (Element Children=[Chars"y"@chars] Name=Name"c"@name)] Name=Name"a"@name)
```

```bash
pego parse -g tags.pego -i '<a><b>x</a></b>'
```

```text
pego: 1:11: mismatched end tag
```

The error is at the position after the offending name: the predicate is evaluated there, and the label is recorded at
that position. An unclosed element fails in `content*`/`close_tag` the ordinary way:

```bash
pego parse -g tags.pego -i '<a><a>x</a>'
```

```text
pego: 1:12: syntax error: expected "<", "</", (?^<)
```

### Indentation blocks

[examples/outline](../../examples/outline/outline.pego) parses an outline whose hierarchy is its indentation:

```pego
// outline.pego
package outline

type Item struct {
    Text     Match
    Children []Item
}

def main = [indent = 0] items:block blank* $$ -> $items

// Items at the current indentation.
def block = (blank* i:item)+ -> map($1, (x) => $x.i)

// A line indented exactly at the current depth, followed by its children.
def item: Item = s:spaces [len($s) == indent] text:line eol cs:children?
    -> new Item{Text: $text, Children: concat($cs)}

// If the next line is indented deeper, read the children with that depth as the new indentation.
// A variable definition is visible only inside this rule, so returning restores the parent's indentation.
def children = &(blank* s:spaces) [len($s) > indent] [indent = len($s)] b:block -> $b

def spaces = @" "*
def line = @(?^\n)+
def eol = "\n" / $$
def blank = (? \t)* "\n"
```

```bash
pego parse -g outline.pego -f sexpr -i $'fruits\n  apple\n  citrus\n    lemon\nvegetables\n'
```

```text
[(Item Children=[(Item Children=[] Text="apple"@line) (Item Children=[(Item Children=[] Text="lemon"@line)] Text="citrus"@line)] Text="fruits"@line) (Item Children=[] Text="vegetables"@line)]
```

How it works. `indent` is the width of the block being read; there is no INDENT/DEDENT token and no stack:

1. `main` defines `indent = 0` and calls `block`. `block` is `item+`; `item` reads the leading spaces into `s` and
   requires `len($s) == indent`, so an item belongs to a block only if it is indented **exactly** like it.
2. After an item's line, `children?` takes a look at the next non-blank line: `&(blank* s:spaces)` captures its
   indentation without consuming it (the capture stays visible after the lookahead).
3. If it is deeper (`[len($s) > indent]`), `[indent = len($s)]` defines a new `indent` **in the `children` invocation**
   and calls `block` again; the nested items see the deeper value, and each of them repeats the process.
4. When the next line is not indented exactly `indent`, `item` fails there, the nested `block` ends, and `children`
   returns. Its `indent` disappears with it, so the caller is back at the shallower value and can read the next
   sibling. A dedent by several levels is just several returns in a row.

The grammar stays context-free in appearance: nothing is "popped". Every `indent` binding lives exactly as long as the
rule invocation that made it.

The failure case is the instructive one. A line that is indented but matches no enclosing level cannot be taken by any
`block`:

```bash
pego parse -g outline.pego -i $'a\n    b\n  c\n'
```

```text
pego: 3:3: syntax error: expected " ", "\n", (? \t)
```

That error is only the farthest point the parser reached, at the end of the indentation of `  c`, where the rules for
more spaces and for a blank line were still trying; it says nothing about the problem. Label the predicate (a failed
predicate records no expected item, so the label alone is recorded at that position):

```pego
// outline.pego (changed rules)
def item: Item = s:spaces [len($s) == indent] #error(message="inconsistent indentation")
    text:line eol cs:children?
    -> new Item{Text: $text, Children: concat($cs)}
```

```bash
pego parse -g outline.pego -i $'a\n    b\n  c\n'
```

```text
pego: 3:3: inconsistent indentation
```

```bash
pego parse -g outline.pego -f sexpr -i $'a\n b\n  c\n d\n'
```

```text
[(Item Children=[(Item Children=[(Item Children=[] Text="c"@line)] Text="b"@line) (Item Children=[] Text="d"@line)] Text="a"@line)]
```

The label does not make valid input fail: at a normal block end, `item` fails at the same predicate, but that only
records a candidate error, which matters only if the parse as a whole fails and this is its farthest failure.

**The Python grammar.** [parsers/python](../../parsers/python/python.pego) uses the same idea at scale. It packs the
indentation of the current block (its column, its column with tabs counted as one, which detects inconsistent use of
tabs, and the number of enclosing blocks) into one variable, `ind`:

- `indented_block` is the `children` rule: it reads the `indentation` of the next line, requires it to be deeper than
  `ind` (with `#error(message="expected an indented block")`), defines the new `ind` and parses the `stmts` of the
  block. A `block` (the part after the `:` of `if`, `def`, ...) calls it, or reads simple statements on the same line,
  so the body of every compound statement is a nested scope.
- `stmt_sep` separates the statements of a block: it reads the indentation of the next line and requires it to be
  exactly `ind`. Clauses that continue a compound statement (`elif`, `else`, `except`) start with `clause_sep`, which
  requires the same indentation as the header.
- A line whose indentation matches no enclosing block is not taken by any `stmt_sep`, so the nested blocks end one
  after the other, and `main` ends with its own message, `invalid syntax`.

```bash
pego parse -g parsers/python/python.pego -f sexpr -i $'if x:\npass\n'
```

```text
pego: 2:1: expected an indented block
```

```bash
pego parse -g parsers/python/python.pego -i $'if x:\n    a = 1\n  b = 2\n'
```

```text
pego: 3:3: invalid syntax
```

### Newlines that depend on context

Python also joins lines inside brackets. The grammar keeps a boolean variable `nl`, and its whitespace rule consumes a
newline only while `nl` is true; every rule that opens a bracket defines `nl = true`. Because a variable is visible
only below the rule that defines it, the flag turns off by itself when the bracket closes:

```pego
// nl.pego
// Statements end at a newline, except inside brackets, where a newline is whitespace.
def main = [nl = false] lines:(ws s:stmt ws "\n")* ws $$ -> map($lines, (x) => $x.s)

def stmt = name ws "=" ws expr
def expr = atom (ws "+" ws atom)*
def atom = name / number / "(" [nl = true] ws expr ws ")"

// A newline is whitespace only while `nl` is true.
def ws = ((? \t)+ / [nl] "\n")*

def name = @((?a-z)+)
def number = @((?0-9)+)
```

```bash
pego parse -g nl.pego -check -i $'x = (1 +\n  2)\ny = 3\n'
```

```text
ok
```

```bash
pego parse -g nl.pego -i $'x = 1 +\n  2\n'
```

```text
pego: 1:8: syntax error: expected "(", (? \t), (?0-9), (?a-z)
```

In the first input the newline inside `( ... )` is whitespace; the one after `)` ends the statement. In the second there
are no brackets, so the newline after `+` is not whitespace and the statement is cut short.

Notice that `[nl]` alone is a predicate: it reads the variable and succeeds if it is `true` (and fails if it is `false`
or not defined).

### Here-documents

A here-document ends at a line that is exactly the delimiter given at the start. The delimiter is data from the input,
so it needs a variable:

```pego
// heredoc.pego
package doc

type Word terminal
type Line terminal
type Heredoc struct { Tag Word, Lines []Line }
type Command struct { Words []Word, Doc *Heredoc }

def main = cs:(-ws c:command)* ws $$ -> map($cs, (x) => $x.c)

def command: Command = ws:(-sp w:word)+ -sp d:heredoc? eol
    -> new Command{Words: map($ws, (x) => $x.w), Doc: $d}

// "<<TAG", the lines up to a line that is exactly TAG, and that closing line.
// A capture of the rule body is not visible inside the repetition, so the tag is kept in a variable.
def heredoc: Heredoc = "<<" tag:word [delim = text($tag)] "\n"
    body:(l:line [text($l) != delim] "\n")* end:line [text($end) == delim] #error(message="unterminated here-document")
    -> new Heredoc{Tag: $tag, Lines: map($body, (x) => $x.l)}

def word: Word = (?a-zA-Z0-9_./-)+
def line: Line = (?^\n)*
def eol = "\n" / $$
def sp = (? \t)*
def ws = (? \t\r\n)*
```

```bash
pego parse -g heredoc.pego -f sexpr -i $'echo a\ncat <<END\nline EOF\nEOF\nEND\necho b\n'
```

```text
[(Command Doc=nil Words=[Word"echo"@word Word"a"@word]) (Command Doc=(Heredoc Lines=[Line"line EOF"@line Line"EOF"@line] Tag=Word"END"@word) Words=[Word"cat"@word]) (Command Doc=nil Words=[Word"echo"@word Word"b"@word])]
```

The line `EOF` inside the document is just text, because the delimiter is `END`. The structure of `heredoc` is the whole
technique:

1. `[delim = text($tag)]` copies the delimiter into a variable right after it is read.
2. Each body line is `l:line [text($l) != delim] "\n"`: the predicate refuses the closing line, which ends the
   repetition (that iteration's capture and consumed input are undone).
3. `end:line [text($end) == delim]` then consumes the closing line.

**Why a variable and not the capture `$tag`?** A capture is visible only in the scope that made it: the rule body or a
single iteration of a repetition. The body of the repetition is a new scope, so `$tag` is not defined there, and this is
detected when the grammar is compiled:

```pego
// scope_bad.pego
def main = t:word ("," w:word [text($w) != text($t)])* $$
def word = @((?a-z)+)
```

```bash
pego parse -g scope_bad.pego -i 'a,b'
```

```text
pego: scope_bad.pego:2:49: undefined capture $t
```

A variable crosses scopes (it is visible to everything below the rule that defines it, including repetition bodies),
which is what makes it the right carrier for "something from the beginning that the end must match".

An unterminated document is reported where it ends:

```bash
pego parse -g heredoc.pego -i $'cat <<EOF\nhello\n'
```

```text
pego: 3:1: unterminated here-document
```

### Long brackets

Lua writes strings as `[[ ... ]]`, `[=[ ... ]=]`, `[==[ ... ]==]`: the closing bracket must have as many `=` as the
opening one, so that the text may contain shorter closers. The number is a variable, and the closing bracket is a rule
that reads it:

```pego
// longstr.pego
def main = long_string $$
def long_string = "[" eq:@"="* "[" [level = len($eq)] body:@(!long_close .)* long_close #error(message="unterminated long string")
def long_close = "]" e:@"="* "]" [len($e) == level]
```

```bash
pego parse -g longstr.pego -f sexpr -i '[==[a]]b]=]c]==]'
```

```text
(Seq (Seq "[" "==" "[" "a]]b]=]c" (Seq "]" "==" "]" e="==")@long_close body="a]]b]=]c" eq="==")@long_string)@main
```

```bash
pego parse -g longstr.pego -i '[==[a]=]'
```

```text
pego: 1:9: unterminated long string
```

`!long_close` is a negative lookahead that calls a rule with captures: that is allowed, because the restriction on
captures in `!` is about the expression written inside it, and the captures of `long_close` belong to `long_close`.
`long_close` reads `level` from `long_string`, which called it through the lookahead.

### Limiting nesting

A counter along the call chain checks a depth limit: every `group` defines a new `depth` from its caller's value, so
siblings at the same level see the same value:

```pego
// depth.pego
// Parentheses nested at most three levels deep.
def main = [depth = 0] group $$

def group = "(" [depth = depth + 1] [depth <= 3] #error(message="nested too deeply") group* ")"
```

```bash
pego parse -g depth.pego -check -i '((()()))'
```

```text
ok
```

```bash
pego parse -g depth.pego -check -i '(((())))'
```

```text
pego: 1:5: nested too deeply
```

## 4. Pitfalls

| Pitfall | What happens | What to do |
|:--|:--|:--|
| A predicate that is not a comparison, such as `[len($s)]` | Any value other than `false` succeeds, so the check never fails | Compare: `[len($s) > 0]` |
| `[$a == $b]` on captures | Compares node identity; always false for two captures | `[text($a) == text($b)]` |
| A misspelled variable name | The predicate fails silently; no error is reported | Check the names; the failed predicate gives only a bare `syntax error` |
| `$label` of the rule body used in a repetition's predicate | `undefined capture` at compile time | Copy it into a variable (`[v = text($label)]`) before the repetition |
| Expecting a callee's `[x = ...]` to be visible after it returns | It is not; the caller sees its own `x` | Return data in the tree (capture and action) |
| Expecting `[n = n + 1]` to count siblings | It defines a new `n` in this invocation, from the caller's value | Count with captures and `len`, or keep the count in the tree |
| A failed predicate without `#error` | `syntax error` with no expected items, or an unrelated message | Label it |
| Variables that change at every call | The memo is keyed by their values, so rules that read them are rarely reused | Keep the values few; build per-node data in actions |
| A capture inside `!(...)` | Compile error (`capture x inside @, -, or ! has no effect`) | Use a predicate on the captured values in a positive context, or move the capture into a rule that `!` calls |
| Tabs in indentation | `spaces` above counts only `" "`, so a tab-indented line is read as text; `len($s)` counts characters | Decide on one convention, or accept `(? \t)*` and compare widths consistently |
