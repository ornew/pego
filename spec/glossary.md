# Glossary

The terms of this specification in alphabetical order. Each entry gives a short
definition and the place where the term is defined.

| Term | Meaning |
|:--|:--|
| action | An expression written after `->` that computes the value of a rule from its captures. See [Actions](actions.md). |
| anchor | A parsing expression that matches a position without consuming input and has no value: `^^`, `$$`, `^`, `$`. See [Anchors](parser-expressions.md#anchors). |
| atomic | `@a`: matches `a` and produces a single terminal with the matched text. See [Atomic](parser-expressions.md#atomic). |
| attribute | An annotation `#name(...)` on a parsing expression that changes how it is parsed: `#error`, `#recover`, `#stream`. See [Attributes](attributes.md). |
| binding level | One precedence level of a Pratt expression, declared with `level`. Levels declared earlier bind more loosely. See [Pratt Expressions](pratt.md#level). |
| capture | A parsing expression `label:a` that records the value of `a` under the name `label`. See [Capture](parser-expressions.md#capture). |
| character class | `(?...)` or `(?^...)`: matches one code point from a set of characters and ranges. See [Character classes](parser-expressions.md#character-classes). |
| choice | `a / b`: an ordered choice, which commits to the first alternative that succeeds. See [Ordered choice](parser-expressions.md#ordered-choice). |
| code point | A Unicode code point; the unit in which the input is matched. See [Input and positions](parsing.md#input-and-positions). |
| concrete syntax tree (CST) | The tree of nodes that parsing expressions produce when no action is involved. See [Values and the concrete syntax tree](parser-expressions.md#values-and-the-concrete-syntax-tree). |
| cut | `--`: commits the enclosing choice to the current alternative. See [Cut](parser-expressions.md#cut). |
| discard | `-a`: matches `a` and has no value. See [Discard](parser-expressions.md#discard). |
| expectation | An item that a syntax error lists as expected at a position: a literal, a character class, `any character` or an anchor. See [Syntax errors](parsing.md#syntax-errors). |
| farthest failure | The farthest position at which parsing failed, with the items expected there; the syntax error reports it. See [Syntax errors](parsing.md#syntax-errors). |
| grammar | A set of type definitions and rule definitions, normally written in a `.pego` file. See [Grammar Files](grammar-files.md). |
| keyword | An identifier reserved by the language, such as `def` and `type`. See [Keywords](lexical.md#keywords). |
| left recursion | A rule that calls itself at the position where it started. It is evaluated by growing a seed. See [Left recursion](parser-expressions.md#left-recursion). |
| lookahead | `&a` and `!a`: tests the input that follows without consuming it. See [Lookahead](parser-expressions.md#lookahead). |
| memoization | Caching the result of a rule at an input position so that the rule is not evaluated twice at that position (packrat parsing). See [Memoization](parsing.md#memoization). |
| node | A value in the tree that parsing produces. Every node has a type and a range in the input. See [Nodes](parsing.md#nodes). |
| operand, operator | The parts of a Pratt expression: an operand is the leading element of an expression, an operator is a prefix, infix or postfix form. See [Pratt Expressions](pratt.md). |
| parsing expression | An expression that matches input; the body of a rule. See [Parsing Expressions](parser-expressions.md). |
| position | A number that identifies a place between two characters of the input, counted in the position unit. See [Input and positions](parsing.md#input-and-positions). |
| position unit | The unit in which positions and lengths in the input are measured: code points or bytes. See [Input and positions](parsing.md#input-and-positions). |
| Pratt expression | A rule body that declares operators by precedence and associativity. See [Pratt Expressions](pratt.md). |
| predicate | A parsing expression `[...]` that evaluates an expression and succeeds or fails without consuming input. See [Predicates and Variables](predicates.md). |
| range | The pair of positions `[start, end)` that a node covers. See [Nodes](parsing.md#nodes). |
| recognition | A parse that only tells whether the input matches, without building a tree. See [Modes](parsing.md#modes). |
| rule | A named parsing expression, defined with `def`. A rule produces a single value when it matches. See [Rule definitions](grammar-files.md#rule-definitions). |
| scope | The place where captures are visible: a rule body or one iteration of a repetition. See [Capture](parser-expressions.md#capture). |
| sequence | `a b`: matches `a` and then `b`. See [Sequence](parser-expressions.md#sequence). |
| start rule | The rule with which parsing begins. It is chosen when the grammar is compiled or parsed, not in the grammar file. See [Start rule](parsing.md#start-rule). |
| struct type | A user-defined node type with named fields. See [Struct types](types.md#struct-types). |
| terminal | A node that holds the matched input text and has no children: a `Match` node or a node of a terminal type. See [Nodes](parsing.md#nodes). |
| terminal type | A user-defined node type for terminals. See [Terminal types](types.md#terminal-types). |
| token | A lexical element of a grammar file: an identifier, a literal, a capture reference or punctuation. See [Tokens](lexical.md#tokens). |
| union type | A type whose values are values of any one of its member types. See [Type aliases and union types](types.md#type-aliases-and-union-types). |
| value | What a parsing expression, a rule or an action produces: a node, `nil`, or an integer, string or boolean inside actions. See [Parsing Expressions](parser-expressions.md#values-and-the-concrete-syntax-tree). |
| variable | A named `int`, `string` or `bool` value defined and read by predicates. See [Variables](predicates.md#variables). |
