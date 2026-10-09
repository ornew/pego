# Brief: translating a region of gram.y into the PEGO grammar of the PostgreSQL parser

SCRATCH = /private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad
W = $SCRATCH/postgresql-work          (the shared tools, read-only for you: never edit anything in W itself)
R = /Users/s27814/src/github.com/ornew/pego/.claude/worktrees/agent-a84132ee088937fd9   (the repository worktree: READ ONLY for you; never run git, never edit anything under R)

You write only inside your own directory D = $W/ag-<group>/ (named in your task). Name every file you create with
the prefix of your group (ag-a-, ag-b1-, ...) when it is outside D. Do not touch the other agents' directories.

## What this is

PEGO (a parser framework for Go with an extended PEG language) is used to write a complete parser of the SQL
dialect of PostgreSQL 18: every statement of src/backend/parser/gram.y, with a typed AST modeled on the nodes of the
raw parse tree (parsenodes.h), checked statement by statement against libpg_query (through pglast), which is the
reference. The core (lexical rules, expressions, SELECT/INSERT/UPDATE/DELETE/MERGE, types, names, shared rules) is
written and works; you translate one REGION of the statements (your task names it) and prove it against the
reference. The coordinator merges the regions into one grammar.

Read first (in this order):
1. $R/spec/*.md and $R/docs/guide/trees-and-actions.md, expressions.md (the PEGO language: rules, captures, actions,
   types, variables and predicates). Read $R/docs/guide/context-sensitive.md for variables.
2. The core grammar: $W/main/parts/*.pego (the files are concatenated in name order). 00-types.pego, 01-types2.pego
   (types), 10-lexical.pego (tokens), 20-names.pego, 30-expr.pego, 31-cexpr.pego, 40-select.pego, 41-clauses.pego,
   50-dml.pego (the DML statements: the style to follow), 51-shared.pego and 52-shared2.pego (rules for you to use).
3. $W/gram.condensed (gram.y without its C code: the productions of your region), and gram.y itself with the actions:
   $SCRATCH/suites/postgres/src/backend/parser/gram.y. The actions say what node is built; the C helper functions
   (makeXxx, processCASbits, SplitColQualList, ...) are at the end of gram.y.
4. $W/schema.txt: every node type of the raw tree with its fields, as the reference prints them.
5. The reference: `$R/.refsql/bin/python $W/p.py 'statement' 'statement'` prints the raw tree (JSON) of statements.
   $SCRATCH/suites/postgres/src/test/regress/sql/*.sql is the corpus; doc/src/sgml/ref/*.sgml documents every
   statement (in the same suites dir).

## The tools (they work, use them)

    sh $W/pgrun.sh D FILES [options]

D is your directory, FILES the scripts of the regression corpus to run (select.sql,join.sql or `all`). It
concatenates D/parts/*.pego into D/postgresql.pego (the rules of the keywords and the type `Any` are GENERATED into
it: write keywords by their names, UPPERCASE, as the tokens of gram.y without the suffix _P: CREATE, TABLE, IF, NOT
EXISTS, DELETE, NULL ...; never write the generated sections), parses every statement of the scripts with it through
the engine, and prints: accepted/rejected, agreement with the reference on acceptance, and for the statements that
both accept whether the trees are equal after the conversion of cmp.py. Options: `-v N` prints the first N tree
differences (path, mine, reference); `-cat REGEX` restricts -v to differences whose path matches; `-rj N` prints N
statements that the reference accepts and the grammar rejects, with the error of the grammar (`-rjpat REGEX`
restricts them to statements matching the regex, for example '^(?i:create +table)'); `-rjmode ok` prints the
statements that the grammar accepts and the reference rejects.

    sh $W/pgcases.sh D [options]

Parses the statements of D/cases/*.sql (your own test scripts) and compares them with the reference the same way.
Write cases/*.sql with one script per group of productions: cover EVERY production of your region (every
alternative, every optional part present and absent, the keywords in different cases, comments between tokens),
the way the documentation shows them (doc/src/sgml/ref/*.sgml has the synopsis of each statement). Also write
invalid statements (the reference decides): the grammar must reject what the reference rejects.

`$W/pego parse -g D/postgresql.pego -s RULE -i 'text' -f sexpr` prints the tree of one rule (the engine, no
generated code). `$W/pego lint -g D/postgresql.pego` lints (do not worry about the warnings that the core has).
Errors of the grammar (syntax, types) are reported with line numbers of D/postgresql.pego.

## Setting up

    mkdir -p D/parts D/ext D/cases && cp $W/main/parts/*.pego D/parts/

Then replace the stub of your group (the file named in your task, it defines the placeholders `StmtStubX` and the
rule `stmt_x`) with the real grammar of your region. You may add more part files with a name between the number
of your stub and the next group (for example 60-a-alter.pego); never edit the files of the core (copy them if you
need a changed version, and tell me what you changed so I can merge it: say so in the report).

## Deliverables

1. Your grammar part(s) in D/parts/ (your stub's file plus any additional), compiling with the core.
2. D/ext/*.py: the extensions of the tree converter (see below) for your node types.
3. D/cases/*.sql: your test scripts.
4. A report (your final message): the statements and nonterminals you cover; for the statements of your region in the
   corpus: how many the reference accepts, how many the grammar accepts, how many trees are equal (run `pgrun.sh D all`
   and filter with -rjpat for the statements of your region; give the commands); for your cases: the same; the list
   of known deviations (what the grammar accepts or rejects differently from the reference, with an example each, and
   what trees differ); the cross-references (rules of other regions you stubbed, changes you want in the core); the
   number of generated lines your grammar adds (see Size).

Do not claim more than you measured. Quote numbers from runs you made, with the command.

## Conventions of the grammar (learn them from the core, here are the rules)

- Tokens consume the white space and comments that follow them: every token rule ends with `s`. Keywords are rules named
  by their upper-case word (SELECT); punctuation: LP RP LB RB COMMA SEMI COLON DOT STAR PLUS MINUS SLASH PERCENT CARET LT GT
  EQ LESS_EQUALS GREATER_EQUALS NOT_EQUALS EQUALS_GREATER COLON_EQUALS TYPECAST, Op; names: ColId, name, ColLabel, attr_name,
  NonReservedWord, type_function_name (all return Ident), Sconst, Iconst, Fconst, Bconst, Xconst, PARAM. Never put
  `s` or white space handling in the middle of rules, never use a string literal for a keyword.
- The AST types are declared in the grammar (type X struct { Field Type, ... }) with the names and fields of the raw
  tree: field `agg_order` is `AggOrder`, `typeName` is `TypeName`, `relpersistence` is `Relpersistence`, `is_local` is
  `IsLocal` (see go2raw in $W/cmp.py: the Go name is the raw name with each underscore-separated part capitalized).
  A field that the raw parser sets to a constant is set to it in the action (Inh: true, Typemod: -1, IsLocal: true...).
  Enumerations are string fields holding the raw enum name ("DROP_CASCADE", "OBJECT_TABLE", "AEXPR_OP"); a field that
  is left empty means the first (default) enumerator of its enum. Booleans are bool, integers int, strings that the
  grammar produces from source names are Ident (a name), Sconst (a string constant) ... Look at how the core does it.
  Names of objects are Ident; a list of names is []Ident (it becomes a list of String nodes in the raw tree).
- A field whose raw value is a `Node *` that can hold different kinds uses a union type (type X = A | B | C). The union
  `Any` of all node types is generated.
- An action is `-> expression` at the end of the rule body, applies to the whole rule, and cannot be attached to one
  alternative of a choice (the parser reads `-> a / b` as a division!): one rule per node-building alternative, or use
  captures and variables in a single rule. Rules must not have two actions.
- Values: `new T{F: x}` builds a node (omitted fields are empty); `list(a, b)`, `concat(l1, l2)` (nil counts as an empty
  list; use it to turn an optional list into a list: `concat($x)`), `map($list, (r) => $r.f)`, `foldl(init, list, (acc,
  i) => ...)`. The usual list of items separated by commas is
      def xs: []T = first:x rest:(-COMMA y:x)* -> concat(list($first), map($rest, (r) => $r.y))
  Inside repetitions discard what you do not need with `-`. `len($x)` is 0 for nil, so booleans from optional
  keywords are `len($k) > 0`. You can read fields of values (`$a.Name`), use `==`, `&&`, `||`, + - * / %, strings.
  A predicate `[expr]` after the captures checks conditions (this is how the checks that gram.y does in its actions,
  and that make the reference reject a statement, are done: `ereport` in gram.y: implement them).
- Variables: `[v = "x"]` defines a variable (immutable, shadowing, undone by backtracking); a rule reads the variables
  of the rules that called it, but a rule cannot set a variable of its caller. Define a default first and set it in the
  alternatives of the same rule (`[m = "FUNC_PARAM_DEFAULT"] (IN [m = "FUNC_PARAM_IN"] / ...)`); reading an undefined
  variable makes the parse fail. Use them to turn keywords into enum strings without a rule per keyword.
- Terminal types: `type T terminal`; a rule declared with a terminal type and NO action returns a terminal holding the
  text it matched INCLUDING the white space of its trailing token. So never write `def name: Ident = ColId` (the text
  would contain trailing white space): leave the type off (`def name = ColId`) or end with an action `-> $x`.
  For an enum-like keyword choice you can also return a terminal and let the converter map its text
  (see DropBehavior in 52-shared2.pego); the converter takes the leading word and compares it lower-cased.
- A sequence that starts with a lookahead (`&x y`) has the type Seq: write `&x v:y -> $v` as a rule of its own.
- Ordered choice commits: when an alternative matches, the choice is not retried with the next one even if what follows
  fails. Put the longer or more specific alternative first, and guard with lookaheads (`!"("`, `&(...)`), as the core
  does (see relation_alias: `!"("`). In gram.y the LALR(1) lookahead decides; you have to decide the same.
- Unreserved keywords can be identifiers: a rule that starts with `ColId` also matches the keywords of the unreserved and
  col_name categories; try the alternatives that start with a keyword first when the keyword could also be a name.
- Left recursion: avoid it (the engine only supports entering a left-recursive cycle at its first rule); use repetitions
  and foldl.
- Keep the grammar COMPACT. The generated Go code costs about 100 lines per rule, 12 per expression and 340 per keyword
  used; the whole parser must stay far below a million lines, so: do not make a rule for what is used once (write
  it inline), merge productions that differ in optional parts into one rule with optional captures instead of one rule
  per alternative whenever the action can be written once, do not write rules that only wrap another rule. Measure:
      $W/pego gen -g D/postgresql.pego -pkg postgresql -types -recognize -nodoc -o D/parser.go; wc -l D/parser.go
  (delete D/parser.go afterwards; do not compile it) and compare with the core alone (the same command on $W/main).
- Rules you need from outside your region: first look at the core (51-shared.pego, 52-shared2.pego, and grep the other
  files). If it is not there and another region owns it (your task says which), define a STUB named exactly as in
  gram.y in a file D/parts/99-stubs.pego (with the types it needs), so that your build compiles; I delete the stubs
  when I merge. Tiny helper nonterminals of gram.y that several regions use (opt_with, opt_drop_behavior, opt_as,
  opt_or_replace, opt_concurrently, add_drop, ...) are NOT shared: define your own with the prefix of your group (a_opt_as)
  unless your task says your region owns them.

## The converter (cmp.py) and its extensions

cmp.py turns the tree of the engine (the grammar's nodes) into the JSON that libpg_query returns, with generic rules:
a struct node becomes {"TypeName": {fields}} (fields renamed by go2raw, empty values omitted, enum defaults filled in),
a list becomes an array, an Ident in a list becomes a String node and plain otherwise, A_Const/Integer/String ... as
in core, a field whose raw type is a concrete node struct (RangeVar, Alias, TypeName, ...) is not wrapped. Where the
raw parser rewrites what the grammar builds (fold of signs, flattening, defaults, computing a field from a list), the
AST keeps the syntactic form and the converter does the same rewriting: write it as an extension, a python file
D/ext/NAME.py (executed in the namespace of cmp.py; see the hooks at the top of cmp.py: TERMINAL_HOOKS[type] =
fn(text, in_list), NODE_HOOKS[type] = fn(node, fields, in_list), POST_HOOKS.append(fn(dict))). Mirror exactly what
gram.y does. Prefer building the exact raw shape in the grammar (an action may read fields and compute) and
use the converter only where the grammar cannot.

## Scope and limits

Every production of your region must be accepted (and rejected where gram.y rejects: its ereport calls in actions,
the %prec/LALR decisions) and its tree must equal the reference's after conversion, except where you document a
deviation. Do not stop at the corpus: your cases/ files prove the productions the corpus does not use. When the
reference rejects a statement for a reason in gram.y (an ereport in an action), the grammar must reject it too when
that is expressible (a predicate); otherwise list it as a deviation. When you finish, run the whole corpus once
(`pgrun.sh D all`) and report the numbers for your region.
