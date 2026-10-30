# Modules and Rules

A module is one program variant in Core IR: all rules of a grammar, in the form one backend consumes, with what the
rules share.

## Module

A module holds the following.

| Part | Meaning |
|:--|:--|
| Rules | The rules by ID, value-free twins included. A rule's ID is its index |
| Starts | The IDs of the start rules |
| Expectations | The strings that nodes record on failure, addressed by an expectation ID. Equal expectations are one |
| Classes | The character classes of the nodes. Equal classes are one class, so classes compare by identity |
| Types | The named types of the grammar, by name |
| Variables | The declared variables, in declaration order, each with a type and an initial value that is an integer, a string, a boolean or nil |
| Call graph | For each rule the rules its body calls, the strongly connected component of each rule, and whether a component is on a cycle |
| Typed | Whether the module carries the checked types of the program |

Two rules are in the same component of the call graph exactly when they call each other, directly or not. A component
is on a cycle when it has more than one rule or its rule calls itself. The callees of a rule are those of its `Call`
and `Nest` nodes, including the nodes of the lines of an Operators Rule.

A module is typed when every term but a lambda, every capture slot and the value of every rule that is not value-free
has a checked type. A checked type is opaque to Core IR: it is compared by identity only.

## Rule

A rule has the following parts.

| Part | Meaning |
|:--|:--|
| ID, name, where | Its identity, and the rule named in the message of a failed action |
| Type | The checked type of the rule's value; absent for a value-free twin |
| Body | The expression the rule matches. An Operators Rule has an `OperatorsRule` node as its whole body |
| Want | Whether the body's value is used. False for a value-free twin, and for a rule whose action reads only captures |
| Scope | The rule's capture slots, in slot order. A slot has a name and a checked type |
| Finish | How the rule makes its value |
| Policy | How the rule is called |
| Twin | The value-free twin of the rule, if it has one |

### Scope

A scope is the list of capture slots that the captures of a rule body write and its terms read. A slot is addressed by
its index and its name. Four constructs have a scope of their own: a rule, a `Repeat` with a scope (each iteration has
fresh captures that the element's value carries), a line of an Operators Rule, and a `Nest` (the scope of the rule it
inlines).

### Finish

The finish of a rule selects how it makes its value.

| Finish | The value is |
|:--|:--|
| None | Nil. A value-free twin |
| Action | The value of the rule's action term. For an Operators Rule, the term reads the folded value of the body as `$value` |
| Splice | The value of the body, which the rule does not wrap. An Operators Rule without an action has this finish |
| Scope | The scope value of the body, with the captures attached, converted to the rule's node type if it names one |

### Policy

| Flag | Meaning |
|:--|:--|
| Rec | The rule is on a cycle of calls; only its entries count against the nesting limit. A pass may remove a call and so a cycle, so Rec may mark a rule that is no longer on one, but never misses one |
| Memo, Transient | Ordinary parses memoize the rule; with Transient, only Documents do |
| Plain | The rule is never memoized outside Documents and has no captures |
| Leader | The rule is a left-recursion leader; the runtime owns its growth |
| Splice | The rule makes no node of its own |
| NoValue | The rule is a value-free twin |
| Seen | The rule's index among the rules memoized from their second call, or -1 |
| Vars | The variables the rule or its callees read |

### Value-Free Twins

A rule that is called where no value is wanted can have a twin: a rule with the same body that builds no value. A twin
has the policy NoValue, no type, the finish None and Want false. A node calls the twin instead of the rule where its own
value is not wanted.

## Shape

The shape of a node is the static form of the value it builds, so that the enclosing node can convert it where it takes
one element.

| Part | Meaning |
|:--|:--|
| Elems | The value is the tuple node of its elements |
| K | The number of valued elements |
| Mode | How the value is made where the node takes one element: none, the value of the element itself, or the tuple node of the elements |
| Tuple | The value of a scope with this body is the tuple node |
| Shared | The tuple node is shared through a splice rule's memo entry |

A node that builds no value has the shape of its expression too: its value is nil, converted like any other.
