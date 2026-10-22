# The `pego` Command

`pego` is the command-line tool of PEGO: it parses input with a grammar, formats and converts grammars, saves compiled
grammars, generates parsers, tests, lints, samples, debugs and runs a language server. This guide shows the workflows
of the commands that work on one grammar and its input. The flags of every command are listed by `pego <command> -h`
and in the generated command reference.

`pego` with no arguments prints the usage and exits with status 1. Every command takes `-h` to list its flags.
Errors are printed to standard error as `pego: ...` and give exit status 1. `fmt` and `convert` accept flags before or
after their file arguments.

A `<grammar>` argument is PEGO source (`.pego`), a grammar in JSON (`.json`) or, for `parse`, `trace`, `profile`,
`explain` and `compile`, a compiled grammar (`.pegoc`). The type is recognized by the contents of a `.pegoc` and by the
`.json` suffix; anything else is parsed as PEGO source. A compiled grammar runs only the parses of the mode it was
saved in, and the commands that need the grammar itself (`convert`, `gen`, `lint`, `sample` and `schema`) reject it.

| Command | Use it to | Read more |
|:--|:--|:--|
| `parse` | Parse input and print the tree, or only check it | [Parse](#parse) |
| `fmt`, `fix`, `convert` | Format grammar source, rewrite old syntax, convert between source and JSON | [Fmt](#fmt) |
| `compile` | Save a compiled grammar of one mode | [Compiled Grammars](compiled-grammars.md) |
| `gen` | Generate a Go or TypeScript parser | [Generating Parsers](code-generation.md), [Generated TypeScript Parsers](typescript.md) |
| `test` | Run a fixture suite | [Testing Grammars](testing-grammars.md) |
| `lint` | Find likely mistakes in a grammar | [Linting Grammars](linting.md) |
| `explain`, `trace`, `profile` | Find out why an input fails or a parse is slow | [Debugging and Profiling](debugging.md) |
| `sample`, `edit-fuzz` | Generate inputs, check incremental parsing | [Sampling Inputs and Fuzzing](sampling-and-fuzzing.md), [Fuzzing Edits](sampling-and-fuzzing.md#fuzzing-edits) |
| `schema`, `schema diff` | Print the output schema of a grammar, compare two schemas | [Output Schemas](output-schemas-and-queries.md#the-command-line) |
| `lsp` | Run the language server | [Editor Support](editor-support.md) |

The examples use the grammar `pairs.pego`:

```pego
type Pair = { Key node[string]; Value node[string] }

@start(default)
rule main = ws ps:pair+ $$ => $ps
rule pair Pair = k:key "=" v:value ";"? ws => Pair{Key: $k, Value: $v}
@start
rule key = [a-zあ-ん]+ #atomic
rule value = [0-9]+ #atomic
rule ws = [ \t\n]*
```

## `parse`

```bash
pego parse -g <grammar> [-s <rule>] [-i <input>] [-f json|sexpr] [-stream] [-check]
           [-unit codepoints|bytes] [-backend closure|bytecode|bytecode-iterative] [-source-info] [-default-value]
```

| Flag | Default | Meaning |
|:--|:--|:--|
| `-g` | (required) | The grammar |
| `-s` | saved in a `.pegoc`, otherwise the default start rule | Start rule |
| `-i` | standard input | The input text. Without `-i`, input is read from standard input. |
| `-f` | `json` | Output format: `json` (indented, like `encoding/json`'s `MarshalIndent`) or `sexpr` (`Node.String()`) |
| `-check` | off | Recognition only: print `ok`, or the syntax error |
| `-unit` | `codepoints` | Position unit |
| `-backend` | chosen for the grammar | `closure`, `bytecode` or `bytecode-iterative` |
| `-source-info` | off | Record the rule of each node and the names of captures, and print them |
| `-default-value` | off | Drop value actions and declared rule types, so the parse returns the default node tree; predicates still run (see [Default Values](../../spec/actions.md#default-values)) |
| `-stream` | off | Print each `#stream` element as soon as it matches, one per line (see [streaming.md](streaming.md)) |

```bash
pego parse -g pairs.pego -f sexpr -i 'abc=12; あい=3'
```

```text
[Pair{Key: "abc", Value: "12"} Pair{Key: "あい", Value: "3"}]
```

```bash
printf 'abc=12; def=3\n' | pego parse -g pairs.pego -f sexpr
```

```text
[Pair{Key: "abc", Value: "12"} Pair{Key: "def", Value: "3"}]
```

```bash
pego parse -g pairs.pego -s key -f sexpr -i 'abc'
```

```text
"abc"
```

```bash
pego parse -g pairs.pego -check -i 'abc=12'
```

```text
ok
```

```bash
pego parse -g pairs.pego -check -i 'abc=x'; echo "exit $?"
```

```text
pego: 1:5: syntax error: expected [0-9]
exit 1
```

Without `-f sexpr` the value is printed as JSON, for `abc=12`:

```json
{
  "start": 0,
  "end": 6,
  "value": [
    {
      "type": "Pair",
      "fields": {
        "Key": {
          "start": 0,
          "end": 3,
          "value": "abc"
        },
        "Value": {
          "start": 4,
          "end": 6,
          "value": "12"
        }
      }
    }
  ]
}
```

A node is an object with `start`, `end` and `value`; a struct is an object with `type` and `fields`. The tree has no rule names
or capture names unless you ask for them with `-source-info`, which adds a `rule` to each node (and `captures` to a tuple that
has them) in JSON, and `@rule` suffixes and `name:` labels in S-expressions:

```bash
pego parse -g pairs.pego -source-info -f sexpr -i 'abc=12'
```

```text
[Pair{Key: "abc"@key, Value: "12"@value}]
```

When the parse recovered from errors with `#recover`, the tree is
printed first and then the errors, with exit status 1, so you can still inspect the tree.

## `fmt`

```bash
pego fmt [-w] [-l] [-check] [<file.pego> ...]
```

Formats PEGO source and **keeps comments**. Without `-w` it prints the result; `-w` rewrites the files in place;
`-l` lists the files whose formatting differs. `-check` lists them too, without changing them, and exits with status 1
when there are any and 2 when a file cannot be read or parsed, so a CI job can run `pego fmt -check *.pego`; `-l` alone
exits with status 0 whatever it lists. Without file arguments it formats standard input (`-w` is then an error). It accepts only `.pego` files; for JSON grammars it points you to `convert`.
Adjacent minus operators stay separated: `- -"a"` remains two discards, and `- -1` remains two unary negations.
The whitespace keeps either expression from becoming `--`, a reserved token that is an error.

A comma-separated list in brackets (call arguments, struct and list constructors, tuples, tuple types, type arguments, lambda parameters and
attribute arguments) stays on one line, like `f(a, b)`, unless it ends in a trailing comma or holds a comment that needs
its own line. Then each element gets a line and a comma, so a trailing comma is the way to ask for the lines:
`f(a, b,)` becomes `f(` with `a,` and `b,` on separate lines and `)` on the last. The comma of a one-element tuple,
`(x,)`, is part of the syntax and does not count.

With `-w`, a changed regular file is written completely to a temporary file in the target directory before replacement.
Permission bits are preserved, read-only files are rejected, and the directory must allow creating and replacing files.
Symlinks keep pointing to the same path and that target is replaced; formatting one hard-linked name leaves the other
names with their original content. Files whose formatting is unchanged are left in place.

```bash
cat messy.pego
```

```text
// A messy grammar.
@start
rule main =    ws   "x"+   ws   $$    // trailing comment
rule ws =  [ \t\n]*
```

```bash
pego fmt -l messy.pego
```

```text
messy.pego
```

```bash
pego fmt -w messy.pego && cat messy.pego
```

```text
// A messy grammar.
@start
rule main =  ws "x"+ ws $$  // trailing comment
rule ws =  [ \t\n]*
```

```bash
pego fmt -l messy.pego     # nothing listed: already formatted
pego fmt -check messy.pego # exit status 0
```

## `fix`

```bash
pego fix [-w] [-l] [<file.pego> ...]
```

Rewrites grammars written in an earlier version of the language to the current syntax. It changes only the tokens
whose spelling changed, adding a space where the new spelling would run into a neighboring token, and keeps every other
byte, so comments and layout survive; run `pego fmt` afterwards if you
also want canonical formatting. The flags and standard-input behavior are those of `fmt`: without `-w` it prints the
result, `-w` rewrites the files in place, and `-l` lists the files that need rewriting. It rewrites:

- the old cut `--` to `#cut`;
- the node members `startPos` and `endPos` to `Start` and `End`;
- the struct declaration `type T struct {...}` to `type T = {...}`, and the alias `type T = U` to `type T ~ U`;
- positional captures `$1`, `$2`, ... to named ones, labeling the element each refers to (with the name of the rule it
  calls when that name is free, and `v1`, `v2`, ... otherwise), and `$0` to a `list(...)` of the labeled elements;
- a lambda parameter written `$p` in the lambda body to `p`;
- a capture read `x.y` to `x.$y`, for every member whose name does not begin with an uppercase letter and is not a
  built-in member;
- the retired type `any` to `_`, when the grammar compiles with `_` everywhere. A value of type `_` needs a cast before
  it is used as another type, so a grammar that needs casts is left as it is, and the compiler reports where;
- a terminal type `type T terminal` to the named node type `type T = node[string]`. A rule declared with it and
  without an action, `rule r T = e`, which produced the text it matched, becomes `rule r T = (e) #atomic` (without
  `#atomic` when `e` is atomic already), and `$x.Value` becomes `text($x)` where `x` captures a call of such a rule. When a
  rule of `T` has captures or makes `T` with a constructor `T{s}`, `T` becomes the struct `type T = { Text string }`
  instead, and those rules make it with `T{Text: ...}`;
- a struct `type T = { Text string }` that a rule makes from the text it matched, `rule r T = t:(e) #atomic =>
  T{Text: text($t)}`, to the named node type `type T = node[string]` with `rule r T = (e) #atomic`, when no other
  constructor makes `T`. A rule written that way without a type is declared with `T`. Reads `x.Text` of its values
  become `text(x)`, and `len(x.Text)` becomes `len(x)`;
- the type names `terminal` and `Match` to `node[string]`, `Seq`, `List` and `Operator` to `node[_]`, and `Error` to
  `error`;
- a bare `node`, which needs a type argument, to `node[string]` where the grammar still type-checks with it, and
  to `node[_]`, a node of any value, otherwise.

The parser rejects each old spelling with a message that points here. Text inside strings, character classes and
comments is left alone. The rewrites other than the cut and type declarations need a grammar that otherwise parses, so
fix syntax errors first. A capture read on a join of node types was optional when some of them lacked the capture,
and `x.$y` requires every one to have it, so the rewritten grammar can report a type error that the old one did not.

```bash
echo 'rule a = "a" -- "b" / "c"' | pego fix
```

```text
rule a = "a" #cut "b" / "c"
```

```bash
echo 'rule pair = key ":" key => list($1, $3)' | pego fix
```

```text
rule pair = key:key ":" v3:key => list($key, $v3)
```

## `convert`

```bash
pego convert [-to pego|json] [-o <file>] <grammar>
```

Converts between PEGO source and the JSON form of the grammar AST. `-to` defaults to the other format (`.pego` becomes
JSON, `.json` becomes PEGO source); a compiled grammar holds no source and is rejected. Without `-o`, the result goes to standard
output. JSON carries the comments that declarations, struct fields and operators rule lines own, and converting it
back to source writes them again, but it carries no layout and no other comments. Keep the
`.pego` as the source of truth.

```bash
pego convert -to json pairs.pego -o pairs.json
pego parse -g pairs.json -f sexpr -i 'abc=12'      # JSON grammars work everywhere a grammar is expected
```

## `compile`

```bash
pego compile -g <grammar> [-s <rule>] [-mode parse|recognize|document] [-source-info] [-default-value] -o <file.pegoc>
```

Saves a compiled grammar; see [compiled grammars](compiled-grammars.md). `-s` sets the default start rule stored in
the file, `-mode` chooses the parses the file serves, `-source-info` makes the parser record the rule of each node and
the names of captures, and `-default-value` makes it return default values (the flag is stored in the file). Both `-g`
and `-o` are required.

| `-mode` | The file serves |
|:--|:--|
| `parse` (default) | `pego parse` and `pego parse -stream` |
| `recognize` | `pego parse -check` |
| `document` | `pego trace`, `pego profile` and `pego explain` |

Another command given the file reports the file's mode and the `-mode` it needs. When `<grammar>` is itself a `.pegoc`,
it is saved again only in its own mode, so pass that mode with `-mode`; otherwise the command reports the mismatch and
writes no output. To save another mode, compile the grammar source.

## `gen`

```bash
pego gen -g <grammar> -module <import path> [-pkg <package>] [-runtime embed|import|only] [-s <rule>] [-o <directory>] [-types] [-recognize] [-source-info] [-default-value]
pego gen -lang ts -g <grammar> -o <directory> [-s <rule>] [-runtime embed|import|only] [-runtime-module <specifier>] [-types] [-recognize] [-observability] [-default-value]
```

Generates a standalone Go parser (see [code-generation.md](code-generation.md)) or, with `-lang ts`, a TypeScript
parser (see [typescript.md](typescript.md)). `-g` is required (except with `-runtime only`), and for Go `-o`, the output directory, and `-module`, the import path of the package (with `-runtime import`, `-pkg` is enough). The Go output is `parser.go`, `api.go` and, with `-runtime embed`, a copy of the runtime in `internal/runtime`; `pego gen` refuses to replace a file in the directory that it did not generate. TypeScript needs `-o` as well and writes `parser.ts`, `runtime.ts` and, with `-types`, `types.ts`; `-runtime import` imports the runtime from `-runtime-module` instead of writing `runtime.ts`. `-source-info` makes the generated Go parser record the rule of each node and the names of captures.
`-default-value` makes the generated parser return default values; it cannot be combined with `-types`, because the
types come from the dropped actions.

A `.pegoc` compiled without `-default-value` cannot be used with `-default-value` in `parse`: the command
asks you to compile it again with `-default-value`.

## `schema`

```bash
pego schema -g <grammar> [-s <rule>] [-all-rules] [-o <file>]
pego schema diff [-s <rule>] [-all-rules] [-f text|json] <before> <after>
```

`schema` prints the [output schema](output-schemas-and-queries.md) of a grammar as JSON, without parsing an input. `-s`
selects the start rule as it does for the other commands, and an empty name selects the default. By default the schema
holds the start rule and the rules it calls, as `Parser.OutputSchema()` does; `-all-rules` adds the unreachable rules
and every declared type, as `pego.WithAllSchemaRules()` does. `-o` writes the file.

`schema diff` compares two schemas and prints one line per added, removed or changed type and rule, for example
`changed rule main`. Each operand is either a schema JSON file, as written by `schema -o`, or a grammar. A file is
recognized by its `"format": "pego-output-schema"` member, so a grammar in JSON is never mistaken for a schema. `-s` and
`-all-rules` apply to the grammar operands only; a schema file already holds the rules it was exported with. With
`-f json`, the output lists the changes with the entries of both sides. The command exits with status 0 whenever both
operands are valid, even when they differ, so a script must compare the output to detect a change. The diff does not
classify whether a change is compatible.

```bash
pego schema -g pairs.pego -o before.json
# edit pairs.pego
pego schema diff before.json pairs.pego        # a schema file and a grammar
pego schema diff -all-rules -f json before.json pairs.pego
```

## `explain`, `trace` and `profile`

```bash
pego explain -g <grammar> [-s <rule>] [-i <input>] [-n <calls>]
pego trace -g <grammar> [-s <rule>] [-i <input>] [-max-depth <n>] [-rule <name>] [-failures] [-f text|json]
pego profile -g <grammar> [-s <rule>] [-i <input>] [-sort <column>] [-n <rows>] [-f text|json]
```

Debugging tools; all three also take `-unit` and `-backend`, and read the input from standard input without `-i`.
`explain` shows which rule calls recorded what a syntax error says was expected, `trace` prints every rule call of a
parse as an indented tree, and `profile` prints the cost of each rule with hints. In Go, the same information comes
from `pego.WithTrace` and `pego.WithProfile`. See [debugging.md](debugging.md).

## `sample`

```bash
pego sample -g <grammar> [-s <rule>] [-n 10] [-seed <n>] [-max-depth <d>] [-max-repeat <r>] [-max-len <bytes>] [-budget <steps>] [-coverage] [-invalid] [-f lines|json]
```

Prints distinct inputs that the grammar accepts, one Go-quoted string per line or as a JSON document; `-invalid`
prints near-miss inputs that it rejects instead. See [sampling-and-fuzzing.md](sampling-and-fuzzing.md).

## Related Topics

- [Running Parsers](running-parsers.md): the Go API behind `parse`.
- [Compiled Grammars](compiled-grammars.md), [Generating Parsers](code-generation.md) and
  [Generated TypeScript Parsers](typescript.md): the commands that ship a grammar.
- [Testing Grammars](testing-grammars.md) and [Debugging and Profiling](debugging.md): the commands that check one.
