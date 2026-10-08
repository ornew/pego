# PEGO for Visual Studio Code

Support for [PEGO](https://github.com/ornew/pego) grammar files (`.pego`):

- syntax highlighting (a TextMate grammar, which works on its own);
- comment toggling, bracket matching and auto-closing pairs;
- through the PEGO language server, `pego lsp`: errors as you type, formatting, go to definition, find references,
  the outline, hover with the type of each rule, rename, completion and semantic highlighting.

See the [editor support guide](https://github.com/ornew/pego/blob/main/docs/guide/editor-support.md) for what the
language server does.

## Requirements

The language server is the `pego` command:

```sh
go install github.com/ornew/pego/cmd/pego@latest
```

VS Code must find `pego` on its `PATH`, or set `pego.server.path` to the command.

## Settings

| Setting | Default | Meaning |
|:--|:--|:--|
| `pego.server.enabled` | `true` | Start the language server. Without it, only highlighting and the editing settings apply |
| `pego.server.path` | `pego` | The `pego` command: a name looked up on the `PATH`, or an absolute path. In an untrusted workspace (Restricted Mode), the workspace's value is ignored, so that opening a folder cannot make VS Code run a program of its choosing |
| `pego.trace.server` | `off` | Log the protocol messages (`messages` or `verbose`) in the *PEGO Language Server* output channel |

The command **PEGO: Restart Language Server** restarts the server, for example after installing a new `pego`.

## Building and installing from source

The extension is plain JavaScript; it has no compile step. With Node.js 20 or later, in this directory:

```sh
npm install            # the language client, and the tools below
npm run check          # validate the JSON files and the TextMate grammar
npm run package        # build pego-<version>.vsix with vsce
code --install-extension pego-0.1.0.vsix
```

To try it without installing, open this directory in VS Code and press F5 (Run Extension): a new window opens with the
extension loaded.

`npm run check` compiles every regular expression of the grammar with Oniguruma, the engine VS Code uses, tokenizes a
sample and checks the scopes of its parts, and tokenizes the example grammars of the repository; it also checks the
manifest and activates the extension with stand-ins for VS Code and the language client.
`npm run check-server -- <path to pego>` starts the language server and talks to it with the JSON-RPC library of the
language client (initialize, diagnostics after an edit, hover, completion, formatting, shutdown).
