// The VS Code extension for PEGO grammar files. It starts the language server, `pego lsp`, for
// .pego files; the language definition and the TextMate grammar are declared in package.json.
'use strict';

const vscode = require('vscode');
const { LanguageClient } = require('vscode-languageclient/node');

/** @type {LanguageClient | undefined} */
let client;

// start starts the language server unless it is disabled.
async function start() {
  const config = vscode.workspace.getConfiguration('pego');
  if (!config.get('server.enabled', true)) {
    return;
  }
  const command = config.get('server.path') || 'pego';
  const c = new LanguageClient(
    'pego', // the prefix of the pego.trace.server setting
    'PEGO Language Server',
    { command, args: ['lsp'] },
    {
      documentSelector: [
        { scheme: 'file', language: 'pego' },
        { scheme: 'untitled', language: 'pego' },
      ],
    },
  );
  try {
    await c.start();
    client = c;
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    vscode.window.showErrorMessage(
      `PEGO: could not start the language server with "${command} lsp": ${message}. ` +
        'Install pego with "go install github.com/ornew/pego/cmd/pego@latest", ' +
        'or set pego.server.path to the pego command.',
    );
  }
}

// stop stops the language server if it runs.
async function stop() {
  const c = client;
  client = undefined;
  if (c) {
    await c.stop();
  }
}

async function restart() {
  await stop();
  await start();
}

/** @param {vscode.ExtensionContext} context */
async function activate(context) {
  context.subscriptions.push(
    vscode.commands.registerCommand('pego.restartServer', restart),
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration('pego.server')) {
        restart();
      }
    }),
  );
  await start();
}

function deactivate() {
  return stop();
}

module.exports = { activate, deactivate };
