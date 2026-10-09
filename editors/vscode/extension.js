// The VS Code extension for PEGO grammar files. It starts the language server, `pego lsp`, for
// .pego files; the language definition and the TextMate grammar are declared in package.json.
'use strict';

const vscode = require('vscode');
const { LanguageClient, State, CloseAction } = require('vscode-languageclient/node');

// 10.1.2 refuses shutdown in StartFailed. Use its protected connection-close
// cleanup after the Node shutdown has retained responsibility for the process.
// Keep ordinary crash recovery unchanged; a failed start must not auto-restart
// while the extension is disposing it. check-client.js exercises the real API.
class PegoLanguageClient extends LanguageClient {
  constructor(...args) {
    super(...args);
    this.shutdownPending = undefined;
    const handler = this.clientOptions.errorHandler;
    this.clientOptions.errorHandler = {
      error: (...args) => handler.error(...args),
      closed: () => this.state === State.StartFailed
        ? { action: CloseAction.DoNotRestart, handled: true }
        : handler.closed(),
    };
  }

  shutdown(mode, timeout) {
    // Initialization failures can call stop before start's rejection settles.
    // Join that cleanup with extension disposal instead of running it twice.
    if (!this.shutdownPending) {
      this.shutdownPending = Promise.resolve().then(() => this.shutdownClient(mode, timeout))
        .finally(() => { this.shutdownPending = undefined; });
    }
    return this.shutdownPending;
  }

  handleConnectionClosed() {
    // A transport close and explicit disposal can race after failed startup.
    // Join their cleanup; the inherited close routine runs within shutdown.
    if (this.state === State.StartFailed) return this.stop();
    return super.handleConnectionClosed();
  }

  async shutdownClient(mode, timeout) {
    if (this.state === State.Starting) await super.start().catch(() => {});
    const failed = this.state === State.StartFailed;
    try {
      await super.shutdown(mode, timeout);
    } catch (err) {
      // Suppress only the dependency's known invalid-state rejection.
      if (!failed || this.state !== State.StartFailed ||
          !/Client is not running and can't be stopped/.test(err.message)) throw err;
    }
    if (failed && this.state === State.StartFailed) await super.handleConnectionClosed();
  }
}

/** @type {LanguageClient | undefined} */
let client;
let clientOutput;
let disposeClient = false;
let active = false;
let lifecycle = Promise.resolve();

// Each caller observes its own failure, while the queue remains usable for a
// later cleanup attempt. Only one transition may change client ownership.
function enqueue(operation) {
  const result = lifecycle.then(operation);
  lifecycle = result.catch(() => {});
  return result;
}

// start starts the language server unless it is disabled.
async function start() {
  if (!active) {
    return;
  }
  const config = vscode.workspace.getConfiguration('pego');
  if (!config.get('server.enabled', true)) {
    return;
  }
  const command = config.get('server.path') || 'pego';
  // The extension owns the channel so dependency cleanup order cannot leave
  // a failed-start channel behind after the client has already become Stopped.
  const output = vscode.window.createOutputChannel('PEGO Language Server', { log: true });
  let c;
  try {
    c = new PegoLanguageClient(
      'pego', // the prefix of the pego.trace.server setting
      'PEGO Language Server',
      { command, args: ['lsp'] },
      {
        outputChannel: output,
        documentSelector: [
          { scheme: 'file', language: 'pego' },
          { scheme: 'untitled', language: 'pego' },
        ],
      },
    );
  } catch (err) {
    output.dispose();
    throw err;
  }
  client = c;
  clientOutput = output;
  disposeClient = true;
  try {
    await c.start();
    disposeClient = false;
  } catch (err) {
    // A client whose server did not start is not reused; dispose of what it holds (its output
    // channel, for one).
    const message = err instanceof Error ? err.message : String(err);
    vscode.window.showErrorMessage(
      `PEGO: could not start the language server with "${command} lsp": ${message}. ` +
        'Install pego with "go install github.com/ornew/pego/cmd/pego@latest", ' +
        'or set pego.server.path to the pego command.',
    );
    await stop();
  }
}

// stop stops the language server if it runs.
async function stop() {
  const c = client;
  if (c) {
    if (disposeClient) {
      await c.dispose();
    } else {
      await c.stop();
    }
    clientOutput.dispose();
    // Failed cleanup retains ownership and blocks replacement until a retry.
    client = undefined;
    clientOutput = undefined;
    disposeClient = false;
  }
}

function restart() {
  return enqueue(async () => {
    await stop();
    await start();
  });
}

/** @param {vscode.ExtensionContext} context */
async function activate(context) {
  active = true;
  context.subscriptions.push(
    vscode.commands.registerCommand('pego.restartServer', restart),
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (active && e.affectsConfiguration('pego.server')) {
        // VS Code does not await event callbacks; handle their failures here.
        return restart().catch((err) => {
          const message = err instanceof Error ? err.message : String(err);
          vscode.window.showErrorMessage(`PEGO: could not update the language server: ${message}`);
        });
      }
    }),
  );
  await enqueue(async () => {
    await stop();
    await start();
  });
}

function deactivate() {
  active = false;
  return enqueue(stop);
}

module.exports = { activate, deactivate };
