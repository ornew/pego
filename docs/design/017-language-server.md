# 017. A language server for PEGO grammars

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-08

## Summary

`pego lsp` runs a [Language Server Protocol](https://microsoft.github.io/language-server-protocol/) (LSP) server for
`.pego` files over standard input and output.
Any editor with an LSP client gets the errors of a grammar as it is typed, formatting, navigation between rules and
types, hover with inferred rule types, rename and completion.

A minimal VS Code extension (`editors/vscode/`) starts the server and adds a TextMate grammar for syntax
highlighting, which works without the server too.

The server is the package `internal/lsp`, written with the standard library only, so the root module gains no
dependency.
How to use it is in the [editor support guide](../guide/editor-support.md).

## Design

### Base protocol and lifecycle

The base protocol is small: each message is a `Content-Length` header, a blank line and a JSON-RPC 2.0 object.
`internal/lsp/jsonrpc.go` reads and writes it; a third-party LSP library would have added a dependency for little
gain.

The server handles messages one at a time, in the order they arrive, on one goroutine.
Every request is answered before the next message is read, so the state needs no locks and the tests are
deterministic.
This is fast enough: analyzing a document after a change takes about 12 ms for the largest example grammars (Go and
Python, 500 to 560 lines; `BenchmarkAnalyze` on an Apple M3 Max), well within the time between keystrokes.
`$/cancelRequest` is therefore ignored: a request is finished before its cancellation is read.

The lifecycle follows the specification:

| State | Requests | Notifications |
|:--|:--|:--|
| before `initialize` | `ServerNotInitialized` (-32002), except `initialize` | dropped, except `exit` |
| initialized | handled; unknown methods get `MethodNotFound` | handled; unknown ones ignored |
| after `shutdown` | `InvalidRequest` | dropped, except `exit` |

`exit` stops the server.
`Serve` returns nil if `shutdown` came first and `ErrExitWithoutShutdown` otherwise, and `pego lsp` exits with status 0
or 1 accordingly, as the specification asks.
A body that is not JSON gets a `ParseError` response with a null id and the server goes on; a header without
`Content-Length` cannot be resynchronized, so the server stops with an error.
A panic in a request handler becomes an `InternalError` response instead of ending the server.
A stack overflow cannot be recovered from, so nothing in the analysis recurses without bound: the lexer skips invalid
characters in a loop, and the parser reports nesting deeper than about 500 levels of parentheses as an error
(`syntax.maxNesting`).
A panic while analyzing a document (a bug) is published as its only diagnostic, and the analysis of the last version
that did not fail keeps answering requests; formatting and rename, which would edit the text that analysis describes,
are refused until the document analyzes again.

### Positions

Three kinds of positions meet in the server:

| Kind | Lines end at | Columns count |
|:--|:--|:--|
| LSP `Position` | `\n`, `\r\n` or `\r` | UTF-16 code units, from 0 |
| PEGO `grammar.Pos` (lexer, parser, compiler) | `\n`, `\r\n` or `\r` | code points, from 1 (the `\r` of `\r\n` is a column) |
| byte offsets into the document text | | bytes |

Every conversion goes through byte offsets (`internal/lsp/text.go`): a `textIndex` holds the offsets at which lines
start and converts each kind of position to and from an offset.
Columns come from checkpoints: every 64 bytes, the index records how many UTF-16 code units and code points precede
that offset, so converting a position walks at most 64 bytes, however long its line (walking from the start of the
line made a file with a long line take seconds to analyze).
The lexer ends a line, and a comment, at a lone `\r` too, as LSP does; it used to end them only at `\n`, so in a
file with old Mac line ends everything after the first comment was part of it.
Formatting keeps the line ends of the file: the formatted lines end like its first line.
A position inside a surrogate pair is taken to be the start of the character, and a position past the end of a line
is the end of the line, as the specification asks.
The server announces `positionEncoding: "utf-16"`, the encoding every client supports.

Documents are synchronized incrementally (`TextDocumentSyncKind.Incremental`): each change replaces a range, given in
LSP positions, of the text after the previous change.

### Analysis

Each version of a document is analyzed once, when a request or a diagnostics notification first needs it
(`analyze` in `internal/lsp/analysis.go`):

1. `syntax.Tokenize` splits the text into tokens, comments included, with their start and end positions.
2. `syntax.ParsePartial` parses the text and returns the grammar even when there are errors.
   The parser skips from an error to the next `def` or `type`, so the grammar holds every definition that parsed;
   navigation, hover and symbols keep working in the rest of a file that has an error.
3. If there is no syntax error, `engine.Compile` compiles and type checks the grammar, and `Program.RuleType` gives
   the declared or inferred type of each rule.
   A grammar with syntax errors is not compiled: the definitions the parser dropped would show up as undefined rules
   and types everywhere they are used.
4. Walking the grammar records each definition and each occurrence of a rule or type name, with its span.
   The AST stores the position of a definition's keyword and of each reference; the span of a name is that of the
   token at that position (or after the keyword).
   Rules and types live in separate namespaces, so `num` and `Num` are different names.

The AST was not changed to hold the positions of names: the tokens give them, and the public AST and its JSON form
stay as they are.

### Diagnostics

Syntax errors are published as the parser reports them: at most one parse error per definition (the parser skips to
the next definition), plus the lexical errors, such as invalid escape sequences; a run of characters that start no
token is one error.
The parser records at most 100 errors and then one that says there are too many, so a file of junk does not flood the
editor.
Compile and type errors are published when there is no syntax error, at most 100 of them and then one that says how
many more there are.
An invalid escape sequence or a run of invalid characters is reported over its text (`syntax.Error.End`); other
errors have only a start position, and their range is the token that starts there, or one character.
The compiler reports errors about a whole definition (such as a rule defined twice) at its `def` or `type` keyword;
those are reported at the definition's name.
Diagnostics are published after `didOpen`, every `didChange` and `didSave`, with the document version, and cleared on
`didClose`.

### Features

| Request | Behavior |
|:--|:--|
| `textDocument/formatting` | `grammar.Format`, like `pego fmt`. A file with syntax errors is not formatted (no edits): the formatter would drop the definitions with errors. The edit replaces only the part between the common prefix and suffix of the old and the new text, so the editor keeps the cursor and folds. A file whose first line ends with `\r\n` keeps `\r\n` line ends. Formatting options (tab size) are ignored: the layout is canonical |
| `definition` | The name in the definition of the rule or type under the cursor. With duplicate definitions, the first |
| `references`, `documentHighlight` | Every occurrence of the name, in the definition (a write highlight) and in references (read highlights) |
| `documentSymbol` | Rules (with their type) and types (struct, terminal or alias), with the fields of structs as children |
| `hover` | For a rule, `def name: Type` with the type declared or inferred, and its documentation comment: the comment lines directly above the definition, or the comment at the end of a one-line definition. For a type, its definition as `pego fmt` prints it. Built-in types, functions, fields and attributes are described |
| `prepareRename`, `rename` | Renames a rule or type defined in the file, everywhere it occurs. Refused, with a message, while the file has syntax errors (occurrences in the definitions the parser dropped would be missed), for built-in and undefined names, and for a new name that is not an identifier, is a keyword, is already defined or used (renaming to the name of an undefined rule would make its references refer to the renamed one), or (for a type) is reserved or does not start with an uppercase letter |
| `completion` | See below |
| `semanticTokens/full` | Tells rule names, type names (built-in ones as `defaultLibrary`), capture labels, predicate variables, fields, built-in functions, attributes, keywords, strings, numbers, character classes and comments apart. Punctuation is left to the TextMate grammar |

Three capabilities of the client change the responses: without `completionItem.snippetSupport`, completion items
insert their label instead of a snippet with placeholders (such as `foldl(${1:init}, ...)`); without
`hierarchicalDocumentSymbolSupport`, the outline is a flat list of `SymbolInformation`, with each field after its
struct; and without `rename.prepareSupport`, the server announces rename without `prepareProvider`, as LSP
requires.

Completion cannot rely on the grammar, because the definition being typed rarely parses.
It looks at the tokens before the cursor instead: inside a comment, string or character class it offers nothing;
after `#`, the attributes; after `->` or inside `[...]` (a value expression), the built-in functions, `new`, `true`,
`false`, `nil` and predicate variables, the capture labels and lambda parameters of the definition after `$`, the
struct types after `new` and the fields after `.`; in the type of a rule or a type definition, the types; after
`infix`, the associativities; elsewhere in a rule body, the rules and the keywords that may come there.
`$` and `.` are trigger characters, so the client asks for completions as soon as they are typed; in a parsing
expression they are the end-of-line anchor and any character, and the server offers nothing directly after them, so
that pressing Enter does not turn `$` into a capture reference or `.` into `.rule`.
Rule and type names come from every `def name` and `type Name` in the file, including definitions that do not parse
yet.

### Editor integration

The server is generic: an editor needs only to start `pego lsp` for `.pego` files.
`editors/vscode/` adds what VS Code needs: a language definition for `.pego` files (line comments, brackets,
auto-closing pairs, indentation after an opening bracket), a TextMate grammar (`syntaxes/pego.tmLanguage.json`), and
a client (`extension.js`) that starts `pego lsp` with `vscode-languageclient`.
The client is plain JavaScript, so the extension has no compile step; its settings select the command
(`pego.server.path`), turn the server off (`pego.server.enabled`) and log the protocol (`pego.trace.server`).
`pego.server.path` names a program to run, so the extension declares it a restricted configuration for untrusted
workspaces: in Restricted Mode, VS Code ignores a value that a workspace sets.
A client whose server fails to start is disposed of, and **PEGO: Restart Language Server** tries again.

Highlighting comes from the TextMate grammar, so it works before the server starts, without it, and in tools that read
TextMate grammars.
The grammar scopes definitions, type expressions (in rule types, aliases and struct bodies, so that a rule named
`string` is not taken for the built-in type), struct fields, capture labels and references, attributes, strings and
character classes with their escape sequences (an unknown escape is marked invalid), and every operator.
Rule references are left unscoped; semantic tokens from the server color them where the editor supports it.

The extension's checks run with Node.js: `npm run check` compiles every regular expression of the grammar with
Oniguruma (`vscode-oniguruma`, the engine VS Code uses), tokenizes a sample with `vscode-textmate` and checks the scope
of each of its parts, and tokenizes the example grammars, none of which may contain an invalid token; it also checks
the manifest and activates the extension with stand-ins for VS Code and the language client;
`npm run check-server` talks to a running `pego lsp` with `vscode-jsonrpc`, the JSON-RPC library of the language
client, to check that the two interoperate.
The node modules and the packaged `.vsix` are not committed.

## Alternatives

- **A third-party LSP or JSON-RPC library** (such as `go.lsp.dev/protocol`): rejected to keep the root module free
  of dependencies. The protocol subset the server needs is a few hundred lines.
- **Analyzing on a background goroutine with a debounce.** Not needed at 12 ms per analysis for the largest
  examples; sequential handling is simpler and makes the results of requests always match the latest text.
- **Compiling the partial grammar of a file with syntax errors**, for type errors in the rest of the file. Rejected:
  every reference to a dropped definition would be reported as undefined.
- **Recording name positions in the AST.** Would change the public `grammar` package for one consumer; the tokens give
  the same information.
- **UTF-8 or UTF-32 positions** (`general.positionEncodings`): only UTF-16 is mandatory for clients, and the
  conversion through byte offsets makes it cheap.

## Limits

- A document is a whole grammar; there are no imports, so there is no workspace-wide analysis.
- Inferred rule types are shown only when the whole grammar compiles.
- Captures (`$name`), Pratt level names and lambda parameters are not navigable; completion of captures after `$`
  lists the labels of the whole definition, not only those in scope.
- Completion is lexical: it can offer a name that is not valid at the cursor in unusual layouts.
- There is no code action, signature help or folding range; editors fold on indentation and brackets.

## Testing

`internal/lsp` is tested with in-process JSON-RPC conversations over pipes: the lifecycle (initialize, shutdown,
exit with and without shutdown, requests before initialize and after shutdown), malformed messages (invalid JSON, a
batch, a wrong `jsonrpc` version, a request without a method, missing and invalid parameters, string ids,
case-insensitive headers, a header without `Content-Length`, the input ending), diagnostics with exact ranges after
non-ASCII and astral characters, incremental changes (several changes in one notification, changes across lines,
`\r\n`), save and close, and every request above.
The position conversions have unit tests of their own, including lone `\r`, offsets inside `\r\n` and positions
inside surrogate pairs.
Every example grammar is analyzed without diagnostics, every name in it resolves, and its formatting edits give what
`pego fmt` prints.
Deliberately breaking the UTF-16 width of astral characters, the ranges of incremental changes, the refusal to format
a file with errors, the line ends kept by formatting, the rename conflict check, the compile step, the exit after
shutdown and the comment check of completion each makes the tests fail.
