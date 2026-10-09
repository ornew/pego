# Editor Support

`pego lsp` is a [Language Server Protocol](https://microsoft.github.io/language-server-protocol/) server for `.pego`
files. Any editor with an LSP client can use it to show the errors of a grammar as you type, format it, jump between
rules and types, show the type a rule produces, rename and complete names.
This guide explains what the server does, how to set up an editor, and its limits. The design is recorded in
[design record 017](../design/017-language-server.md).

- [Installing the server](#installing-the-server)
- [What the server does](#what-the-server-does)
- [Setting up an editor](#setting-up-an-editor)
- [Limits](#limits)
- [Troubleshooting](#troubleshooting)

## Installing the server

The server is part of the `pego` command:

```sh
go install github.com/ornew/pego/cmd/pego@latest
pego lsp     # speaks LSP on standard input and output; editors start it for you
```

`go install` puts `pego` in `$(go env GOPATH)/bin` (or `$GOBIN`). Editors start the command by name, so that
directory must be on the `PATH` the editor sees, or the editor must be given the full path.

`pego lsp` takes no options. It ignores the arguments that clients add, such as `--stdio` and `--clientProcessId=N`:
standard input and output are the only transport.

## What the server does

A `.pego` file is a whole grammar, so the server analyzes each open file on its own, again after every change.

| Feature | What you see |
|:--|:--|
| Diagnostics | Syntax errors as you type. When the file has none, the errors of compiling and type checking it too: undefined rules and types, rules defined twice, type errors in actions, invalid attributes. The range of each error is the token it is about |
| Formatting | The file formatted like `pego fmt`, keeping comments. A file with syntax errors is left as it is. Line ends stay `\r\n` if the file uses them. The editor's tab size is ignored: the layout is canonical |
| Go to definition | From a rule or type name to its definition |
| Find references, highlights | Every place a rule or type is used. Rules and types are different names, so `num` and `Num` are kept apart |
| Document symbols | The outline of the file: rules with their types, types, and the fields of struct types |
| Hover | For a rule, `def name: Type` with its declared or inferred type, and its documentation comment. For a type, its definition. For built-in types, functions (`len`, `foldl`, ...), node fields (`startPos`, ...) and attributes (`#error`, ...), a description |
| Rename | Renames a rule or type everywhere in the file. A new name that is invalid, a keyword or already taken is refused |
| Completion | Rule names in rule bodies, types where a type is expected, attributes after `#`; in actions and predicates, captures after `$`, built-in functions, `new`, struct types after `new` and fields after `.`; and keywords |
| Semantic highlighting | Rule names, type names, captures, fields and built-in names in their own colors, if the editor supports semantic tokens |

White space and `//` comments may separate `def`, `type` or `new` from the following name. Symbols,
navigation, references and rename still identify the name itself; rename leaves the comment text intact.

### Documentation comments

The comment lines directly above a definition are its documentation, shown on hover and in completion. A
definition on one line can instead have a comment at the end of the line:

```pego
// A number: one or more digits.
// Leading zeros are allowed.
def number: Number = @(?0-9)+

type Number terminal // the text of a number
```

A blank line between the comments and the definition ends the documentation.

### Inferred types

Hover shows the type of every rule, also the rules without a declared type:

```pego
def digits = (?0-9)+    // hover: def digits: []Match (inferred)
def pair = a:digits "," b:digits -> list($a, $b)
```

The types come from the type checker, so they are shown only while the whole grammar compiles. While it has errors,
hover shows the declared type, or says that the type is not known yet.

## Setting up an editor

The examples below start `pego lsp` for files ending in `.pego`.

### Visual Studio Code

The repository has an extension in [`editors/vscode`](../../editors/vscode/README.md). It adds the `pego` language
(line comments, bracket matching and auto-closing pairs), syntax highlighting with a TextMate grammar, and starts
`pego lsp` for `.pego` files. It is not published to the Marketplace; build and install it from a checkout with
Node.js 20 or later:

```sh
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

### Other editors

Configure a language server with the command `pego lsp` for the files ending in `.pego`. The server announces
everything it supports in its reply to `initialize`, and uses UTF-16 positions, which every client supports.

## Limits

- Each file is a whole grammar: there is no analysis across files.
- Inferred types, type errors and other compile errors appear only when the file has no syntax error. A file with a
  syntax error is not compiled, because the definitions the parser skipped would show up as undefined everywhere.
- While the file has syntax errors, rename is refused, and navigation and hover work for the definitions that parse.
  The parser skips from an error to the next `def` or `type`, so one broken definition does not hide the others.
- Captures (`$name`), Pratt level names and lambda parameters cannot be navigated to or renamed. Completion after `$`
  lists the labels of the whole definition, whether or not they are in scope.
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
