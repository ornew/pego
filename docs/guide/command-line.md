# The `pego` Command

`pego` is the command-line tool of PEGO: it parses input with a grammar, formats and converts grammars, saves compiled
grammars, generates parsers, tests, lints, samples, debugs and runs a language server. This guide shows the workflows
of the commands that work on one grammar and its input. The flags of every command are listed by `pego <command> -h`
and in the generated command reference.

`pego` with no arguments prints the usage and exits with status 1. Every command takes `-h` to list its flags.
Errors are printed to standard error as `pego: ...` and give exit status 1. `fmt` and `convert` accept flags before or
after their file arguments.

A `<grammar>` argument is PEGO source (`.pego`), a grammar in JSON (`.json`) or a compiled grammar (`.pegoc`). The type
is recognized by the contents of a `.pegoc` and by the `.json` suffix; anything else is parsed as PEGO source.

| Command | Use it to | Read more |
|:--|:--|:--|
| `parse` | Parse input and print the tree, or only check it | [Parse](#parse) |
| `fmt`, `fix`, `convert` | Format grammar source, rewrite old syntax, convert between source and JSON | [Fmt](#fmt) |
| `compile` | Save a compiled grammar | [Compiled Grammars](compiled-grammars.md) |
| `gen` | Generate a Go or TypeScript parser | [Generated Go Parsers](code-generation.md), [Generated TypeScript Parsers](typescript.md) |
| `test` | Run a fixture suite | [Testing Grammars](testing-grammars.md) |
| `lint` | Find likely mistakes in a grammar | [Linting Grammars](linting.md) |
| `explain`, `trace`, `profile` | Find out why an input fails or a parse is slow | [Debugging and Profiling](debugging.md) |
| `sample`, `edit-fuzz` | Generate inputs, check incremental parsing | [Sampling Inputs and Fuzzing](sampling-and-fuzzing.md), [Incremental Parsing](incremental-editing.md#fuzzing-edits) |
| `lsp` | Run the language server | [Editor Support](editor-support.md) |

The examples use the grammar `pairs.pego`:

```pego
type Pair = { Key Match; Value Match }

rule main = ws ps:pair+ $$ => $ps
rule pair Pair = k:key "=" v:value ";"? ws => Pair{Key: $k, Value: $v}
rule key = [a-zあ-ん]+ #atomic
rule value = [0-9]+ #atomic
rule ws = [ \t\n]*
```

## `parse`

```bash
pego parse -g <grammar> [-s <rule>] [-i <input>] [-f json|sexpr] [-stream] [-check]
           [-unit codepoints|bytes] [-backend closure|bytecode|bytecode-iterative]
```

| Flag | Default | Meaning |
|:--|:--|:--|
| `-g` | (required) | The grammar |
| `-s` | saved in a `.pegoc`, otherwise `main` | Start rule |
| `-i` | standard input | The input text. Without `-i`, input is read from standard input. |
| `-f` | `json` | Output format: `json` (indented, like `encoding/json`'s `MarshalIndent`) or `sexpr` (`Node.String()`) |
| `-check` | off | Recognition only: print `ok`, or the syntax error |
| `-unit` | `codepoints` | Position unit |
| `-backend` | chosen for the grammar | `closure`, `bytecode` or `bytecode-iterative` |
| `-stream` | off | Print each `#stream` element as soon as it matches, one per line (see [streaming.md](streaming.md)) |

```bash
$ pego parse -g pairs.pego -f sexpr -i 'abc=12; あい=3'
[(Pair Key="abc"@key Value="12"@value) (Pair Key="あい"@key Value="3"@value)]
$ printf 'abc=12; def=3\n' | pego parse -g pairs.pego -f sexpr
[(Pair Key="abc"@key Value="12"@value) (Pair Key="def"@key Value="3"@value)]
$ pego parse -g pairs.pego -s key -f sexpr -i 'abc'
"abc"@key
$ pego parse -g pairs.pego -check -i 'abc=12'
ok
$ pego parse -g pairs.pego -check -i 'abc=x'; echo "exit $?"
pego: 1:5: syntax error: expected [0-9]
exit 1
```

Without `-f sexpr` the tree is printed as JSON, for instance (shortened) for `abc=12`:

```json
{
  "type": "List",
  "start": 0,
  "end": 6,
  "children": [
    {
      "type": "Pair",
      "start": 0,
      "end": 6,
      "fields": {
        "Key": { "type": "Match", "rule": "key", "start": 0, "end": 3, "text": "abc" },
        "Value": { "type": "Match", "rule": "value", "start": 4, "end": 6, "text": "12" }
      }
    }
  ]
}
```

(The tool prints the nested objects over several lines.) When the parse recovered from errors with `#recover`, the tree is
printed first and then the errors, with exit status 1, so you can still inspect the tree.

## `fmt`

```bash
pego fmt [-w] [-l] [-check] [<file.pego> ...]
```

Formats PEGO source and **keeps comments**. Without `-w` it prints the result; `-w` rewrites the files in place;
`-l` lists the files whose formatting differs. `-check` lists them too, without changing them, and exits with status 1
when there are any and 2 when a file cannot be read or parsed, so a CI job can run `pego fmt -check *.pego`; `-l` alone
exits with status 0 whatever it lists. Without file arguments it formats standard input (`-w` is then an error). It accepts only `.pego` files; for JSON and compiled grammars it points you to `convert`.
Adjacent minus operators stay separated: `- -"a"` remains two discards, and `- -1` remains two unary negations.
The whitespace keeps either expression from becoming `--`, a reserved token that is an error.

With `-w`, a changed regular file is written completely to a temporary file in the target directory before replacement.
Permission bits are preserved, read-only files are rejected, and the directory must allow creating and replacing files.
Symlinks keep pointing to the same path and that target is replaced; formatting one hard-linked name leaves the other
names with their original content. Files whose formatting is unchanged are left in place.

```bash
$ cat messy.pego
// A messy grammar.
rule main =    ws   "x"+   ws   $$    // trailing comment
rule ws =  [ \t\n]*
$ pego fmt -l messy.pego
messy.pego
$ pego fmt -w messy.pego && cat messy.pego
// A messy grammar.
rule main =  ws "x"+ ws $$  // trailing comment
rule ws =  [ \t\n]*
$ pego fmt -l messy.pego     # nothing listed: already formatted
$ pego fmt -check messy.pego # exit status 0
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
- the struct declaration `type T struct {...}` to `type T = {...}`, and the alias `type T = U` to `type T ~ U`;
- positional captures `$1`, `$2`, ... to named ones, labeling the element each refers to (with the name of the rule it
  calls when that name is free, and `v1`, `v2`, ... otherwise), and `$0` to a `list(...)` of the labeled elements;
- a lambda parameter written `$p` in the lambda body to `p`;
- a capture read `x.y` to `x.$y`, for every member whose name does not begin with an uppercase letter and is not a
  built-in field;
- the deprecated type `any` to `_`, where the grammar still compiles with `_`. A value of type `_` needs a cast before
  it is used as another type, so a use that would need one keeps `any`, as does a rule type or cast that must produce
  a node; `pego lint` reports them.

The parser rejects each old spelling with a message that points here. Text inside strings, character classes and
comments is left alone. The rewrites other than the cut and type declarations need a grammar that otherwise parses, so
fix syntax errors first. A capture read on a union of node types was optional when some of them lacked the capture,
and `x.$y` requires every one to have it, so the rewritten grammar can report a type error that the old one did not.

```bash
$ echo 'rule a = "a" -- "b" / "c"' | pego fix
rule a = "a" #cut "b" / "c"
$ echo 'rule pair = key ":" key => list($1, $3)' | pego fix
rule pair = key:key ":" v3:key => list($key, $v3)
```

## `convert`

```bash
pego convert [-to pego|json] [-o <file>] <grammar>
```

Converts between PEGO source and the JSON form of the grammar AST. `-to` defaults to the other format (`.pego` becomes
JSON, `.json` becomes PEGO source), and is required for a `.pegoc` input. Without `-o`, the result goes to standard
output. JSON and compiled grammars carry no comments or layout, so converting back to source gives canonical formatting
without comments; keep the `.pego` as the source of truth.

```bash
pego convert -to json pairs.pego -o pairs.json
pego parse -g pairs.json -f sexpr -i 'abc=12'      # JSON grammars work everywhere a grammar is expected
pego convert -to pego pairs.pegoc                  # recover the source of a compiled grammar that has the AST
```

## `compile`

```bash
pego compile -g <grammar> [-s <rule>] [-no-ast] -o <file.pegoc>
```

Saves a compiled grammar; see [compiled grammars](compiled-grammars.md). `-s` sets the default start rule stored in
the file, and `-no-ast` omits the AST. When the input is an AST-less `.pegoc`, pass `-no-ast`; the command rejects
AST-less input without it rather than implying that the missing AST was restored. A version 3 recognition bundle is
preserved when re-saving with `-no-ast`. Both `-g` and `-o` are required.

When `<grammar>` is an AST-less `.pegoc`, `-no-ast` is required. Without it, compilation reports the missing AST and
writes no output. The option preserves bytecode-only form; it cannot restore source grammar data.

## `gen`

```bash
pego gen -g <grammar> -pkg <package> [-s <rule>] [-o <file>] [-types] [-recognize]
pego gen -lang ts -g <grammar> [-s <rule>] [-o <file>] [-recognize]
```

Generates a standalone Go parser (see [code-generation.md](code-generation.md)) or, with `-lang ts`, a TypeScript
module (see [typescript.md](typescript.md)). `-g` is required, and `-pkg` for Go; without `-o` the code goes to
standard output.

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
- [Compiled Grammars](compiled-grammars.md), [Generated Go Parsers](code-generation.md) and
  [Generated TypeScript Parsers](typescript.md): the commands that ship a grammar.
- [Testing Grammars](testing-grammars.md) and [Debugging and Profiling](debugging.md): the commands that check one.
