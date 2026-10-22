# Editor Support

`pego lsp` is a [Language Server Protocol](https://microsoft.github.io/language-server-protocol/) server for `.pego`
files. Any editor with an LSP client can use it to show the errors of a grammar as you type, format it, jump between
rules and types, show the type a rule produces, rename and complete names.
This guide explains what the server does, how to set up an editor, and its limits. The design is recorded in
[design record 017](../design/017-language-server.md).

- [Installing the server](#installing-the-server)
- [What the server does](#what-the-server-does)
- [Experimental lint diagnostics and actions](#experimental-lint-diagnostics-and-actions)
- [Setting up an editor](#setting-up-an-editor)
- [Limits](#limits)
- [Troubleshooting](#troubleshooting)

## Installing the Server

The server is part of the `pego` command:

```bash
go install github.com/ornew/pego/cmd/pego@latest
pego lsp     # speaks LSP on standard input and output; editors start it for you
```

`go install` puts `pego` in `$(go env GOPATH)/bin` (or `$GOBIN`). Editors start the command by name, so that
directory must be on the `PATH` the editor sees, or the editor must be given the full path.

`pego lsp` takes no options. It ignores the arguments that clients add, such as `--stdio` and `--clientProcessId=N`:
standard input and output are the only transport.

## What the Server Does

A `.pego` file is a whole grammar, so the server analyzes each open file on its own, again after every change.

| Feature | What you see |
|:--|:--|
| Diagnostics | Syntax errors as you type. When the file has none, the errors of compiling and type checking it too: undefined rules and types, rules defined twice, type errors in actions, invalid attributes. The range of each error is the token it is about |
| Formatting | The file formatted like `pego fmt`, keeping comments. A file with syntax errors is left as it is. Line ends stay `\r\n` if the file uses them. The editor's tab size is ignored: the layout is canonical |
| Go to definition | From a rule or type name to its definition |
| Find references, highlights | Every place a rule or type is used. Rules and types are different names, so `num` and `Num` are kept apart |
| Document symbols | The outline of the file: rules with their types, types, and the fields of struct types |
| Hover | For a rule, `rule <name> [<Type Expression>] = <Rule Expression>` with its declared or inferred type, and its documentation comment. For a type, its definition. For built-in types (including `error`, the type of recovered errors), functions (`len`, `foldl`, ...), node members (`Start`, `End` and `Value`) and attributes (`@error`, ...), a description |
| Rename | Renames a rule or type everywhere in the file. A new name that is invalid, a keyword or already taken is refused. A [hidden rule](../../spec/actions.md#hidden-rules) keeps its leading `_` and a visible rule cannot gain one, because that changes the nodes a parse makes |
| Completion | Rule names in rule bodies, types where a type is expected, type names and a `FieldName Type` snippet at field-name positions in struct declarations, attributes after `@` and pragmas after `#`; in actions and predicates, captures after `$`, node members (`Start`, `End` and `Value`), struct fields and the captures read as `x.$y` after `.`, built-in functions, and struct types in constructors; and keywords |
| Semantic highlighting | Rule names, type names, captures, fields and built-in names in their own colors, if the editor supports semantic tokens |

White space may separate Declaration Statement keywords from names; comments
inside statement headers are semantic errors. Line comments use `//`, and nested block
comments use `/* ... */`. Comments are permitted in expressions and at statement
boundaries. Symbols, navigation, references and rename identify names even in a
partially invalid document; rename leaves comment text intact.

### Documentation Comments

The comment lines directly above a definition are its documentation, shown on hover and in completion. A
definition on one line can instead have a comment at the end of the line:

```pego
// A number: one or more digits.
// Leading zeros are allowed.
rule number = [0-9]+ #atomic

type Number = node[string] // the text of a number
```

A blank line between the comments and the definition ends the documentation.

### Inferred Types

Hover shows the type of every rule, also the rules without a declared type. An inferred type is the
type of the rule's value; in it, a rule with a declared type stands for that type, and a rule without
one for its name:

```pego
type Num = node[string]
rule digits = [0-9]+ // hover: rule digits node[[]node[string]] (inferred)
rule num Num = digits #atomic
rule pair = a:num "," b:digits => list($a, $b) // hover: rule pair [](Num | digits) (inferred)
```

The types come from the type checker, so they are shown only while the whole grammar compiles. While it has errors,
hover shows the declared type, or says that the type is not known yet. A hidden rule without an action has no type
of its own, and hover says so.

### Experimental Lint Diagnostics and Actions

The VS Code extension can opt into grammar lint diagnostics, guarded explanations, and responsive analysis. Set `pego.server.experimentalLint` to `true`, then restart the language server. The setting defaults to `false`; with it off, lint diagnostics and these code actions are not enabled.

When enabled, lint findings appear only after the grammar parses and compiles successfully. Select a lint diagnostic and request a quick fix to see a guarded explanation. The explanation is shown as a message; it does not edit the grammar. The server can return a versioned `// lint:ignore <check>` edit after verifying that the edited grammar remains valid and the targeted finding disappears. The VS Code extension shows it as a quick fix that inserts the directive on the line above the finding. It applies the edit only if the document is still at the analyzed version: the version is checked immediately before the edit is handed to VS Code, which refuses an edit made against a different version. If the document changed, nothing is inserted and the quick fix must be requested again. This relies on VS Code behavior that is not part of its documented API. The server does not turn the linter's explanatory `Fix` text into an edit.

Explanations are tied to the document version and analysis that produced the diagnostic; the extension rechecks them before display and drops stale requests. The setting also enables responsive analysis: newer document versions can replace pending analysis and supersede stale results. Cancellation is cooperative, and some parsing, compilation, lint, and request-processing phases do not yet check for cancellation internally, so an already-running phase may need to finish before the server responds.

## Setting Up an Editor

The examples below start `pego lsp` for files ending in `.pego`.

### Visual Studio Code

The repository has an extension in [`editors/vscode`](../../editors/vscode/README.md). It adds the `pego` language
(line comments, bracket matching and auto-closing pairs), syntax highlighting with a TextMate grammar, and starts
`pego lsp` for `.pego` files. It is not published to the Marketplace; build and install it from a checkout with
Node.js 20 or later:

```bash
cd editors/vscode
npm install
npm run package                        # builds pego-0.1.0.vsix
code --install-extension pego-0.1.0.vsix
```

To try it without installing, open `editors/vscode` in VS Code and press F5: a window with the extension loaded opens.

| Setting | Default | Meaning |
|:--|:--|:--|
| `pego.server.enabled` | `true` | Start the language server. Without it, only highlighting and the editing settings apply |
| `pego.server.path` | `pego` | The `pego` command: a name looked up on the `PATH`, or an absolute path. In an untrusted workspace (Restricted Mode), the workspace's value is ignored, so that opening a folder cannot make VS Code run a program of its choosing |
| `pego.trace.server` | `off` | Log the protocol messages (`messages` or `verbose`) in the *PEGO Language Server* output channel |

**PEGO: Restart Language Server** in the command palette restarts the server, for example after installing a new
`pego`. Highlighting does not need the server, so it works even when `pego` is not installed; the extension then
shows an error once and offers no diagnostics.

### Neovim

With Neovim 0.11 or later, in `init.lua`:

```lua
vim.filetype.add({ extension = { pego = "pego" } })
vim.lsp.config("pego", {
  cmd = { "pego", "lsp" },
  filetypes = { "pego" },
  root_markers = { ".git" },
})
vim.lsp.enable("pego")
```

Formatting is `vim.lsp.buf.format()`, rename `vim.lsp.buf.rename()`. Set `commentstring` for the file type
(`vim.bo.commentstring = "// %s"` in `after/ftplugin/pego.lua`) so that `gc` comments lines.

### Helix

In `languages.toml`:

```toml
[language-server.pego]
command = "pego"
args = ["lsp"]

[[language]]
name = "pego"
scope = "source.pego"
file-types = ["pego"]
comment-token = "//"
indent = { tab-width = 4, unit = "    " }
language-servers = ["pego"]
```

### Emacs

With Eglot (built into Emacs 29 and later):

```elisp
(define-derived-mode pego-mode prog-mode "PEGO"
  "Major mode for PEGO grammars."
  (setq-local comment-start "// "))
(add-to-list 'auto-mode-alist '("\\.pego\\'" . pego-mode))
(with-eval-after-load 'eglot
  (add-to-list 'eglot-server-programs '(pego-mode "pego" "lsp")))
```

Then `M-x eglot` in a `.pego` buffer, or add `eglot-ensure` to `pego-mode-hook`.

### Other Editors

Configure a language server with the command `pego lsp` for the files ending in `.pego`. The server announces
everything it supports in its reply to `initialize`, and uses UTF-16 positions, which every client supports.

## Limits

- Each file is a whole grammar: there is no analysis across files.
- Inferred types, type errors and other compile errors appear only when the file has no syntax error. A file with a
  syntax error is not compiled, because the definitions the parser skipped would show up as undefined everywhere.
- While the file has syntax errors, rename is refused, and navigation and hover work for the definitions that parse.
  After an error, the parser skips to the next `rule`, `operators`, or `type`
keyword, so one broken definition does not hide later Declaration Statements.
- Captures (`$name`), Operators Rule level names and lambda parameters cannot be navigated to or renamed. Completion after `$`
  lists the labels of the whole definition, but not those that the action cannot read: an [inline
  action](../../spec/actions.md#inline-value-actions) reads the captures made inside its parentheses, and the action
  of the rule those made outside every inline action. Lambda parameters, which are written
  without `$`, are offered with the other names inside the lambda's body.
- After `.`, completion offers `$y` for the captures of the rule a capture `$x` labels; after another receiver, such
  as a lambda parameter, it offers the captures inside the labelled groups of the definition and those of the rules
  repeated directly after a label, which is a guess at the elements the lambda reads.
- Completion looks at the tokens before the cursor, not at a parse of the definition being written, so in unusual
  layouts it can offer a name that does not fit.

## Troubleshooting

- **Nothing happens.** Check that the editor finds the command: run `pego lsp` in a terminal (it waits for input;
  press Ctrl-C), and give the editor the full path if it does not see your `PATH` (in VS Code, `pego.server.path`).
  In VS Code, set `pego.trace.server` to `messages` and look at the *PEGO Language Server* output channel.
- **The server exits with status 1.** It does so when the editor sends `exit` without `shutdown` first, or when the
  input ends; the editor restarts it.
- **Formatting does nothing.** The file has a syntax error; fix the errors the editor shows first.
- **No types on hover.** The grammar has an error somewhere; types are inferred when it compiles.
