# Glossary

The terms of this specification in alphabetical order. Each entry gives a short
definition and the place where the term is defined.

| Term | Meaning |
|:--|:--|
| action | An umbrella term for a Predicate Action and a Value Action. See [Actions](language/actions.md). |
| Action Expression | A value-level expression evaluated inside a Predicate Action or Value Action. See [Actions](language/actions.md). |
| anchor | A Rule Expression that matches a position without consuming input and has no value: `^^`, `$$`, `^`, `$`. See [Anchors](language/parser-expressions.md#anchors). |
| atomic | `a #atomic`: matches `a` and produces a single string node. See [Atomic](language/pragmas.md#atomic). |
| attribute | An `@` annotation for diagnostics or metadata, such as `@error(...)`; it does not change matching behavior. See [Attributes](language/attributes.md). |
| binding level | One precedence level of an Operators Rule, declared with `level`. Levels declared earlier bind more loosely. See [Operators Rule](language/operators-rule.md#level). |
| capture | A Rule Expression `label:a` that names the element `a` as `label`, so that an action can read it as `$label`. See [Capture](language/parser-expressions.md#capture). |
| character class | `[...]` or `[^...]`: matches one code point from a set of characters and ranges. See [Character Matching](language/parser-expressions.md#character-classes). |
| choice | `a / b`: an ordered choice, which commits to the first alternative that succeeds. See [Ordered choice](language/parser-expressions.md#ordered-choice). |
| code point | A Unicode code point; the unit in which the input is matched. See [Input and positions](runtime/parsing.md#input-and-positions). |
| compiled grammar | A grammar saved as a `.pegoc` file holding the bytecode of one mode (parse, recognize or document), with no grammar AST, that loads without parsing grammar source. See [Compiled Grammars](runtime/compiled-grammars.md). |
| concrete syntax tree (CST) | The tree of nodes that Rule Expressions produce when no action is involved: string nodes, tuples for sequences and lists for repetitions. See [Values and the concrete syntax tree](language/parser-expressions.md#values-and-the-concrete-syntax-tree). |
| contextual word | A grammar word interpreted as syntax only in a Declaration Statement or Operators Rule position; the same spelling may be an identifier elsewhere. See [Contextual Words](language/lexical.md#contextual-words). |
| Core IR | The intermediate representation of a checked grammar, whose nodes make their effects explicit. See [PEGO Core IR Specification](core-ir/README.md). |
| cut | `#cut`: commits the enclosing choice to the current alternative. See [Cut](language/parser-expressions.md#cut). |
| Declaration Statement | A Type Declaration Statement or Rule Declaration Statement in a `.pego` file. See [Grammar Files](language/grammar-files.md#structure). |
| discard | `-a`: matches `a` and has no value. See [Discard](language/parser-expressions.md#discard). |
| Document | A mutable text together with its parse result, updated by edits and reparsed incrementally. See [Incremental Documents](runtime/documents.md). |
| edit | The replacement of a range of a Document's text; the next parse reuses the results that the edit did not affect. See [Edits](runtime/documents.md#edits). |
| effect | One thing a Core IR node can do besides succeed or fail, such as consuming input, recording expectations or writing a capture. See [Attributes and Effects](core-ir/attributes.md#effects). |
| expectation | An item that a syntax error lists as expected at a position: a literal, a character class, `any character` or an anchor. See [Syntax errors](runtime/parsing.md#syntax-errors). |
| farthest failure | The farthest position at which parsing failed, with the items expected there; the syntax error reports it. See [Syntax errors](runtime/parsing.md#syntax-errors). |
| generated parser | A standalone Go or TypeScript parser written for one grammar by `pego gen`. See [Generated Parsers](runtime/generated-parsers.md). |
| grammar | An optional package clause followed by Type and Rule Declaration Statements, normally written in a `.pego` file. See [Grammar Files](language/grammar-files.md). |
| join type | The join `T \| U` of types: a type whose values are the values of any one of its member types. See [Join Types](language/types.md#join-types). |
| left recursion | A rule that calls itself at the position where it started. It is evaluated by growing a seed. See [Left recursion](language/parser-expressions.md#left-recursion). |
| lookahead | `&a` and `!a`: tests the input that follows without consuming it. See [Lookahead](language/parser-expressions.md#lookahead). |
| meet type | The meet `T & U` of types: a type whose values are the values of both. See [Top, Bottom, Join and Meet](language/types.md#top-bottom-join-and-meet). |
| memoization | Caching the result of a rule at an input position so that the rule is not evaluated twice at that position (packrat parsing). See [Memoization](runtime/parsing.md#memoization). |
| named node type | A Type Definition `type N = node[U]`: a type distinct from `node[U]` and from every other named node type. Its values are nodes that carry the name `N`. See [Named Node Types](language/types.md#named-node-types). |
| node | A value together with the range of input that it covers: a string, a tuple or a list. See [Nodes and Values](runtime/nodes.md#node-model). |
| operand, operator | The parts of an Operators Rule: an operand is the leading element of an expression, an operator is a prefix, infix or postfix form. See [Operators Rule](language/operators-rule.md). |
| Operators Rule | A rule declaration that declares operators by precedence and associativity. See [Operators Rule](language/operators-rule.md). |
| position | A number that identifies a place between two characters of the input, counted in the position unit. See [Input and positions](runtime/parsing.md#input-and-positions). |
| position unit | The unit in which positions and lengths in the input are measured: code points or bytes. See [Input and positions](runtime/parsing.md#input-and-positions). |
| pragma | A `#` annotation that changes expression behavior or structure, such as `#atomic`, `#recover`, `#repeat(...)` or `#stream`, which follow an expression, and `#cut`, which stands in place of one. See [Pragmas](language/pragmas.md). |
| predicate | A Rule Expression `{ term }` that evaluates a Predicate Action without consuming input; a single term retains the established predicate semantics. See [Predicates and Variables](language/predicates.md). |
| Predicate Action | `{ term }`: an action in a Rule Expression that evaluates without consuming input or producing a value, and fails if the result is false or an error. See [Predicates and Variables](language/predicates.md#predicate-actions). |
| prefix parse | A parse that matches the start rule at the beginning of the input and reports how much it consumed, instead of requiring the whole input. See [Prefix Parsing](runtime/parsing.md#prefix-parsing). |
| range | The pair of positions `[start, end)` that a node covers. See [Nodes and Values](runtime/nodes.md#node-model). |
| recognition | A parse that only tells whether the input matches, without building a tree. See [Modes](runtime/parsing.md#modes). |
| recovery | `#recover`: after a failure in an element, skips input to a synchronization point, keeps an error value and continues. See [`#recover`](language/pragmas.md#recover). |
| rule | A named Rule Expression, defined with `rule <name> [<Type Expression>] = <Rule Expression>`. A rule produces a single value when it matches. See [Rule Declaration Statements](language/grammar-files.md#rule-declaration-statements). |
| Rule Declaration Statement | A statement `rule ...` that declares a rule: a Rule Definition. See [Rule Declaration Statements](language/grammar-files.md#rule-declaration-statements). |
| Rule Definition | `rule a = b`: defines the rule `a` by a Rule Expression or an Operators Rule. See [Rule Declaration Statements](language/grammar-files.md#rule-declaration-statements). |
| Rule Expression | An expression that matches input; the body of an ordinary rule. See [Rule Expressions](language/parser-expressions.md). |
| scope | The place where captures are visible: a rule body or one iteration of a repetition. See [Capture](language/parser-expressions.md#capture). |
| source information | An opt-in record of the rule that produced each node and of the names of captures. See [Source Information](runtime/nodes.md#source-information). |
| sequence | `a b`: matches `a` and then `b`. See [Sequence](language/parser-expressions.md#sequence). |
| start rule | The rule with which parsing begins. It is chosen when the grammar is compiled or parsed, not in the grammar file. See [Start rule](runtime/parsing.md#start-rule). |
| stream | A `#stream` repetition whose elements are handed to the caller as soon as they match, so that earlier input can be released. See [Streaming](runtime/streaming.md). |
| struct type | A user-defined type of values with named fields, which an action builds. See [Struct types](language/types.md#struct-types). |
| string node | A node whose value is the matched input text: the node of a literal, a character class, `.` or an atomic expression. Its type is `node[string]`. See [Nodes and Values](runtime/nodes.md#node-model). |
| terminal | A string node. `Node.IsTerminal` reports it. See [Nodes and Values](runtime/nodes.md#node-model). |
| token | A lexical element of a grammar file: an identifier, a literal, a capture reference or punctuation. See [Tokens](language/lexical.md#tokens). |
| tuple | The value of a sequence: the values of its elements, in order. A rule without a valued element produces the empty tuple, the unit `()`. See [Nodes and Values](runtime/nodes.md#node-model). |
| Type Alias | `type a ~ b`: gives the type `b` another name `a`, and defines no type. See [Type Aliases](language/types.md#type-aliases). |
| Type Declaration Statement | A statement `type ...` that declares a type name: a Type Definition or a Type Alias. See [Type Declaration Statements](language/types.md#type-declaration-statements). |
| Type Definition | `type a = b`: defines a new type `a`, distinct from every other type. See [Type Definitions](language/types.md#type-definitions). |
| Type Expression | An expression that specifies a type, including the declared value type of a rule or the right-hand side of a Type Declaration Statement. See [Type System](language/types.md). |
| value | What a Rule Expression, a rule or an action produces: a node, `nil`, an error value, a struct, or an integer, string or boolean inside actions. See [Rule Expressions](language/parser-expressions.md#values-and-the-concrete-syntax-tree). |
| Value Action | `=> term`: the action that follows a complete Rule Expression and computes the value of the rule from its captures. See [Actions](language/actions.md#whole-rule-value-actions). |
| variable | A named `int`, `string` or `bool` value declared by a `var` declaration, bound and read by actions. See [Variables](language/predicates.md#variables). |
