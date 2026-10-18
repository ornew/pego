# Parsing Structured Input

The first lesson matched fixed text. This lesson introduces the pieces of the grammar language that real input needs:
character classes, repetition, shaping the tree, and white space and comments, and ends with a parser for a small
configuration language.

## What You Will Build

A grammar for files of `key = value` lines with comments, and the tree it produces.

## Before You Start

Complete [Getting Started With PEGO](getting-started.md). Work in the same directory.

## Building Blocks

The grammar language has a few more pieces than literals. This lesson introduces them on a tiny language: a lowercase
word, an equals sign, and a number, as in `port=8080`.

### Rules, Character Classes and Repetition

A **character class** `[...]` matches one character from a set written as characters and ranges: `[a-z]` is a
lowercase ASCII letter and `[0-9]` is a digit. `+` after an expression repeats it one or more times (`*` is zero or
more, `?` is zero or one). A rule can call another rule by name. Save as `kv.pego`; each step below replaces its
contents with the next version:

```pego
rule main = key "=" value
rule key = [a-z]+
rule value = [0-9]+
```

```bash
pego parse -g kv.pego -f sexpr -i 'port=8080'
```

```text
(["p" "o" "r" "t"] "=" ["8" "0" "8" "0"])
```

It works, but the tree is noisy: a repetition produces a list with one element per iteration, so `key` is a list of
single-letter strings.

### Atomic: `#atomic`

`expr #atomic` matches `expr` and produces one string node holding all of the matched text, without building a node for each
part. `[a-z]+ #atomic` applies atomic construction suppression to the repeated character class:

```pego
rule main = key "=" value
rule key = [a-z]+ #atomic
rule value = [0-9]+ #atomic
```

```text
("port" "=" "8080")
```

### Discard: `-`

The `=` is needed to parse the input but is of no interest in the tree. `-expr` matches `expr` and throws the result
away:

```pego
rule main = key -"=" value
rule key = [a-z]+ #atomic
rule value = [0-9]+ #atomic
```

```text
("port" "8080")
```

### Captures: `name:`

A **capture** `name:expr` gives the value of `expr` a name. The tree is unchanged, because the value stays where it is in
the sequence, but actions refer to it as `$name`, as the next step shows. The names appear in the output when you ask for
source information with `-source-info`:

```pego
rule main = k:key "=" v:value
rule key = [a-z]+ #atomic
rule value = [0-9]+ #atomic
```

```text
("port" "=" "8080")
```

```bash
pego parse -g kv.pego -f sexpr -source-info -i 'port=8080'
```

```text
(k:"port"@key "=" v:"8080"@value)@main
```

The label and the colon must be written together (`k:key`, not `k :key`).

### Value Actions and Struct Types: `=>`

An **action** builds the value of a rule yourself. It is written after `=>` inside the rule body, and can create a value of a struct type
declared with `type`. This is the grammar from the README (`kv.pego`, final version):

```pego
type Pair = { Key node[string]; Value node[string] }

rule main = k:[a-z]+ #atomic "=" v:[0-9]+ #atomic => Pair{Key: $k, Value: $v}
```

```bash
pego parse -g kv.pego -f sexpr -i 'abc=12'
```

```text
Pair{Key: "abc", Value: "12"}
```

`Pair{...}` creates a `Pair` struct, and `$k` and `$v` refer to the captures. The literal `=` has no name, so it is
simply left out of the result. The tree now contains exactly what the language is about, and nothing else.

Two things worth knowing at this point:

- An action applies to the **whole** rule body. A rule such as `rule r = a / b => ...` runs the action
  whatever alternative matched; to build different values for different alternatives, give each alternative its own
  rule.
- Types are checked when the grammar is compiled, so a typo in a field name is reported before any input is parsed.
  You will see examples in [Building Typed Trees](typed-trees.md).

## A Configuration Language

Now a more realistic language: a file of `key = value` lines with comments.

```text
# server settings
host = "localhost"
port = 8080
debug = true
```

Create `example.conf` with that content:

```bash
cat > example.conf <<'EOF'
# server settings
host = "localhost"
port = 8080
debug = true
EOF
```

Save the following grammar as `config-cst.pego`. It produces plain syntax-tree nodes, with no actions; the next lesson adds types.

```pego
rule main = -ws entry* $$

rule entry = k:key -ws -"=" -ws v:value -ws

rule value = number / string / bool
rule key = [a-z_]+ #atomic
rule number = [0-9]+ #atomic
rule string = ("\"" [^"]* "\"") #atomic
rule bool = ("true" / "false") #atomic

rule ws = (blank / comment)*
rule blank = [ \t\r\n]+
rule comment = "#" [^\n]*
```

```bash
pego parse -g config-cst.pego -f sexpr < example.conf
```

```text
([("host" "\"localhost\"") ("port" "8080") ("debug" "true")])
```

Without `-i`, `pego parse` reads the input from standard input. The new pieces:

- **Whitespace is explicit.** PEGO does not skip whitespace for you. The rule `ws` matches any run of blanks, newlines
  and comments, and the grammar says where it is allowed. Each `entry` consumes the whitespace after itself, and
  `main` starts with one `-ws` for the whitespace before the first entry. Whitespace is discarded with `-`, so it never
  appears in the tree.
- **`[^...]` is a negated class.** `[^"]` matches any character except a quote, and `[^\n]` matches any
  character except a line feed, so `comment` reads to the end of the line. Escapes such as `\n` and `\"` work in literals and classes.
- **`/` is ordered choice.** `value` tries `number`, then `string`, then `bool`, and commits to the first that
  matches. Order matters: with `("a" / "ab") $$`, the input `ab` fails because `"a"` matches first and is never
  reconsidered.

  ```pego
  rule main = ("a" / "ab") $$
  ```

  ```bash
  pego parse -g ab.pego -f sexpr -i 'ab'
  ```

  ```text
  pego: 1:2: syntax error: expected end of input
  ```

  Write `("ab" / "a")` (the longer alternative first) to accept both.
- **`$$` is the end of input.** It matches without consuming anything and has no value. The start rule must consume the
  whole input anyway, so `$$` is optional here, but writing it makes the intent clear. (`^^` is the beginning of the
  input.)
- **`entry*` is a repetition.** The whole file is a tuple holding one list of entries. An empty file is valid and
  gives `([])`.

## Next Steps

The tree above is a faithful picture of the text. [Building Typed Trees](typed-trees.md) reshapes it into the data the
program wants. The guides [Rule Expressions](../guide/rule-expressions.md) and [Shaping Trees](../guide/shaping-trees.md)
cover the expressions of this lesson in depth.
