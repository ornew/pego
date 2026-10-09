// Exercises the installed language client's real startup and cleanup paths.
// Only the VS Code host and the rejected-initialize peer are controlled.
'use strict';

const assert = require('node:assert/strict');
const Module = require('module');
const path = require('node:path');
const { PassThrough } = require('node:stream');
const { within } = require('./extension-harness');

const clients = [], channels = [], diagnostics = [], peers = [];
const commands = {};
let command = '/no/such/pego-lifecycle-check';
const disposable = () => ({ dispose() {} });
const host = {
  version: '1.106.0', LogLevel: { Info: 3 }, CodeActionKind: {},
  env: { language: 'en', appName: 'PEGO lifecycle check' },
  workspace: {
    workspaceFolders: [], textDocuments: [], notebookDocuments: [],
    getConfiguration: () => ({ get: (key, fallback) => key === 'server.path' ? command : fallback }),
    onDidChangeConfiguration: disposable,
  },
  commands: { registerCommand: (name, f) => { commands[name] = f; return disposable(); } },
  languages: { createDiagnosticCollection: () => {
    const collection = { disposed: 0, dispose() { this.disposed++; }, clear() {} };
    diagnostics.push(collection);
    return collection;
  } },
  window: {
    tabGroups: { all: [], onDidChangeTabs: disposable }, visibleTextEditors: [],
    onDidChangeVisibleTextEditors: disposable,
    showErrorMessage: () => Promise.resolve(),
    createOutputChannel: () => {
      const channel = { logLevel: 3, disposed: 0, error() {}, warn() {}, info() {},
        debug() {}, trace() {}, appendLine() {}, show() {}, dispose() { this.disposed++; } };
      channels.push(channel);
      return channel;
    },
  },
};
// The real client loads protocol converters even when startup fails; these
// classes need no editor behavior for the paths exercised here.
for (const name of ['CompletionItem', 'CodeLens', 'DocumentLink', 'InlayHint',
  'TypeHierarchyItem', 'CallHierarchyItem', 'CodeAction', 'Diagnostic',
  'SymbolInformation', 'CancellationError']) host[name] = class {};
host.EventEmitter = class { event = disposable; fire() {} dispose() {} };
for (const name of ['onDidOpenTextDocument', 'onDidCloseTextDocument',
  'onDidChangeTextDocument', 'onWillSaveTextDocument', 'onDidSaveTextDocument',
  'onDidOpenNotebookDocument', 'onDidCloseNotebookDocument',
  'onDidChangeNotebookDocument']) host.workspace[name] = disposable;

const load = Module._load;
Module._load = function (request, ...rest) {
  if (request === 'vscode') return host;
  return load.call(this, request, ...rest);
};
const api = require('vscode-languageclient/node');
const rpc = require('vscode-jsonrpc/node');
class ProbeClient extends api.LanguageClient {
  constructor(...args) {
    super(...args);
    this.cleared = 0;
    this.registerFeature({ fillInitializeParams() {}, initialize() {}, clear: () => { this.cleared++; } });
    clients.push(this);
  }
  async start() {
    try {
      await super.start();
    } catch (err) {
      // Exercise transport-close cleanup winning the race with extension
      // disposal, using the actual protected client callback.
      if (command === '/closed-before-disposal') await this.handleConnectionClosed();
      throw err;
    }
  }
  async createMessageTransports(encoding) {
    // Force the real diagnostic provider to own a collection before failure.
    assert(this.diagnostics);
    if (command !== '/initialize-rejected') return super.createMessageTransports(encoding);
    const input = new PassThrough(), output = new PassThrough();
    const peer = rpc.createMessageConnection(new rpc.StreamMessageReader(output), new rpc.StreamMessageWriter(input));
    peer.onRequest('initialize', () => { throw new rpc.ResponseError(-32603, 'initialize rejected'); });
    peer.listen();
    peers.push(peer);
    return { reader: new rpc.StreamMessageReader(input), writer: new rpc.StreamMessageWriter(output) };
  }
}
Module._load = function (request, ...rest) {
  if (request === 'vscode') return host;
  if (request === 'vscode-languageclient/node') return { ...api, LanguageClient: ProbeClient };
  return load.call(this, request, ...rest);
};
const extension = require(path.resolve(process.env.PEGO_EXTENSION || path.join(__dirname, '..', 'extension.js')));
Module._load = load;

async function main() {
  // Actual ENOENT, then an actual JSON-RPC initialization error. A third
  // failed launch proves both cleaned-up clients permit a new attempt.
  await within(extension.activate({ subscriptions: [] }));
  command = '/initialize-rejected';
  await within(commands['pego.restartServer']());
  command = '/no/such/pego-lifecycle-check';
  await within(commands['pego.restartServer']());
  command = '/closed-before-disposal';
  await within(commands['pego.restartServer']());
  await within(extension.deactivate());
  assert.equal(clients.length, 4);
  for (const client of clients) {
    assert.equal(client.state, api.State.Stopped);
    assert.equal(client.cleared, 1, 'failed client features cleared once');
    assert.equal(client.serverProcess, undefined);
  }
  assert.equal(channels.length, 4);
  for (const channel of channels) assert.equal(channel.disposed, 1, 'output disposed once');
  assert.equal(diagnostics.length, 4);
  for (const collection of diagnostics) assert.equal(collection.disposed, 1, 'diagnostics disposed once');
  console.log('installed client startup failure and retry ok');
}

main().catch((err) => { console.error(err); process.exitCode = 1; })
  .finally(() => { for (const peer of peers) peer.dispose(); });
