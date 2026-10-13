# Glossary

The terms of this specification in alphabetical order. Each entry gives a short
definition and the place where the term is defined.

| Term | Meaning |
|:--|:--|
| Action Expression | A value-level expression evaluated inside a Predicate Action or Value Action. See [Actions](actions.md). |
| action | An umbrella term for a Predicate Action and a Value Action. See [Actions](actions.md). |
| anchor | A Rule Expression that matches a position without consuming input and has no value: `^^`, `$$`, `^`, `$`. See [Anchors](parser-expressions.md#anchors). |
| atomic | `a #atomic`: matches `a` and produces a single text-only `Match`. See [Atomic](pragmas.md#atomic). |
| attribute | An `@` annotation for diagnostics or metadata, such as `@error(...)`; it does not change matching behavior. See [Attributes](attributes.md). |
| binding level | One precedence level of an Operators Rule, declared with `level`. Levels declared earlier bind more loosely. See [Operators Rule](operators-rule.md#level). |
| capture | A Rule Expression `label:a` that records the value of `a` under the name `label`. See [Capture](parser-expressions.md#capture). |
| character class | `[...]` or `[^...]`: matches one code point from a set of characters and ranges. See [Character Matching](parser-expressions.md#character-classes). |
| choice | `a / b`: an ordered choice, which commits to the first alternative that succeeds. See [Ordered choice](parser-expressions.md#ordered-choice). |
| code point | A Unicode code point; the unit in which the input is matched. See [Input and positions](parsing.md#input-and-positions). |
| concrete syntax tree (CST) | The tree of nodes that Rule Expressions produce when no action is involved. See [Values and the concrete syntax tree](parser-expressions.md#values-and-the-concrete-syntax-tree). |
| cut | `--`: commits the enclosing choice to the current alternative. See [Cut](parser-expressions.md#cut). |
| Definition Statement | A Type Definition Statement or Rule Definition Statement in a `.pego` file. See [Grammar Files](grammar-files.md#structure). |
| discard | `-a`: matches `a` and has no value. See [Discard](parser-expressions.md#discard). |
| expectation | An item that a syntax error lists as expected at a position: a literal, a character class, `any character` or an anchor. See [Syntax errors](parsing.md#syntax-errors). |
| farthest failure | The farthest position at which parsing failed, with the items expected there; the syntax error reports it. See [Syntax errors](parsing.md#syntax-errors). |
| grammar | An optional package clause followed by Type and Rule Definition Statements, normally written in a `.pego` file. See [Grammar Files](grammar-files.md). |
| contextual word | A grammar word interpreted as syntax only in a Definition Statement or Operators Rule position; the same spelling may be an identifier elsewhere. See [Contextual Words](lexical.md#contextual-words). |
| left recursion | A rule that calls itself at the position where it started. It is evaluated by growing a seed. See [Left recursion](parser-expressions.md#left-recursion). |
| lookahead | `&a` and `!a`: tests the input that follows without consuming it. See [Lookahead](parser-expressions.md#lookahead). |
| memoization | Caching the result of a rule at an input position so that the rule is not evaluated twice at that position (packrat parsing). See [Memoization](parsing.md#memoization). |
| node | A value in the tree that parsing produces. Every node has a type and a range in the input. See [Nodes](parsing.md#nodes). |
| operand, operator | The parts of an Operators Rule: an operand is the leading element of an expression, an operator is a prefix, infix or postfix form. See [Operators Rule](operators-rule.md). |
| Rule Expression | An expression that matches input; the body of an ordinary rule. See [Rule Expressions](parser-expressions.md). |
| pragma | A postfix `#` annotation that changes expression behavior or structure, such as `#atomic`, `#recover`, `#repeat(...)` or `#stream`. See [Pragmas](pragmas.md). |
| position | A number that identifies a place between two characters of the input, counted in the position unit. See [Input and positions](parsing.md#input-and-positions). |
| position unit | The unit in which positions and lengths in the input are measured: code points or bytes. See [Input and positions](parsing.md#input-and-positions). |
| Operators Rule | A rule declaration that declares operators by precedence and associativity. See [Operators Rule](operators-rule.md). |
| predicate | A Rule Expression `{ term }` that evaluates a Predicate Action without consuming input; a single term retains the established predicate semantics. See [Predicates and Variables](predicates.md). |
| range | The pair of positions `[start, end)` that a node covers. See [Nodes](parsing.md#nodes). |
| recognition | A parse that only tells whether the input matches, without building a tree. See [Modes](parsing.md#modes). |
| Rule Definition Statement | A statement that defines an ordinary rule or an Operators Rule. See [Rule Definition Statements](grammar-files.md#rule-definition-statements). |
| rule | A named Rule Expression, defined with `rule <name> [<Type Expression>] = <Rule Expression>`. A rule produces a single value when it matches. See [Rule Definition Statements](grammar-files.md#rule-definition-statements). |
| scope | The place where captures are visible: a rule body or one iteration of a repetition. See [Capture](parser-expressions.md#capture). |
| sequence | `a b`: matches `a` and then `b`. See [Sequence](parser-expressions.md#sequence). |
| start rule | The rule with which parsing begins. It is chosen when the grammar is compiled or parsed, not in the grammar file. See [Start rule](parsing.md#start-rule). |
| struct type | A user-defined node type with named fields. See [Struct types](types.md#struct-types). |
| terminal | A node that holds the matched input text and has no children: a `Match` node or a node of a terminal type. See [Nodes](parsing.md#nodes). |
| terminal type | A user-defined node type for terminals. See [Terminal types](types.md#terminal-types). |
| token | A lexical element of a grammar file: an identifier, a literal, a capture reference or punctuation. See [Tokens](lexical.md#tokens). |
| Type Expression | An expression that specifies a type, including the declared value type of a rule or a type alias. See [Type Definitions](types.md#type-definitions). |
| union type | A type whose values are values of any one of its member types. See [Type aliases and union types](types.md#type-aliases-and-union-types). |
| value | What a Rule Expression, a rule or an action produces: a node, `nil`, or an integer, string or boolean inside actions. See [Rule Expressions](parser-expressions.md#values-and-the-concrete-syntax-tree). |
| variable | A named `int`, `string` or `bool` value defined and read by predicates. See [Variables](predicates.md#variables). |
