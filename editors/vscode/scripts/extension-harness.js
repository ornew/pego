// Loads the actual extension with controlled API/client stand-ins. No VS Code
// window or language-server process is started.
'use strict';

const Module = require('module');
const path = require('path');

function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function plan(options = {}) {
  return { started: deferred(), stopping: deferred(), disposing: deferred(), ...options };
}

// A timeout reports a stuck transition; deferred gates, not elapsed time, order
// the lifecycle operations in the tests.
async function within(promise) {
  let timer;
  try {
    return await Promise.race([
      promise,
      new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('transition did not settle')), 5000); }),
    ]);
  } finally {
    clearTimeout(timer);
  }
}

function loadExtension(plans = [], options = {}) {
  const h = {
    settings: { 'server.enabled': true, 'server.path': '/pego' },
    commands: {}, events: [], clients: [], running: new Set(), resources: new Set(),
    created: 0, maxRunning: 0, information: [], applied: [], appliedVersions: [], requests: [],
  };
  const record = (event) => { if (options.record !== false) h.events.push(event); };
  const vscode = {
    workspace: {
      textDocuments: [],
      // Like the extension host, stamp the edit with the version of the document
      // held at the moment of the call. The main thread (h.mainVersion, when
      // set) refuses an edit whose stamp differs from its own model version.
      applyEdit: (edit) => {
        const stamps = edit.entries().map(([uri]) =>
          vscode.workspace.textDocuments.find((d) => d.uri.toString() === uri.toString())?.version);
        h.applied.push(edit); h.appliedVersions.push(stamps);
        const main = h.mainVersion;
        return Promise.resolve(!main || stamps.every((v) => v === undefined || v === main()));
      },
      onDidCloseTextDocument: (f) => { h.closedDocument = f; return { dispose() {} }; },
      getConfiguration: () => ({ get: (key, fallback) => key in h.settings ? h.settings[key] : fallback }),
      onDidChangeConfiguration: (f) => { h.configuration = f; return { dispose() {} }; },
    },
    commands: {
      registerCommand: (name, f) => { h.commands[name] = f; return { dispose() {} }; },
    },
    window: {
      showInformationMessage: (message) => { h.information.push(message); return Promise.resolve(); },
      showErrorMessage: (message) => { record('error: ' + message); return Promise.resolve(); },
      createOutputChannel: () => ({ dispose() {} }),
    },
  };
  vscode.WorkspaceEdit = class {
    constructor() { this.text = new Map(); }
    set(uri, edits) { this.text.set(uri, edits); }
    insert(uri, position, newText) { this.set(uri, [{ range: { start: position, end: position }, newText }]); }
    replace(uri, range, newText) { this.set(uri, [{ range, newText }]); }
    entries() { return [...this.text.entries()]; }
  };
  vscode.Uri = { parse: (value) => ({ toString: () => value }) };
  vscode.Range = class { constructor(sl, sc, el, ec) { this.start = { line: sl, character: sc }; this.end = { line: el, character: ec }; } };
  vscode.Position = class { constructor(line, character) { this.line = line; this.character = character; } };
  class LanguageClient {
    constructor(id, name, server, clientOptions) {
      this.id = h.created++;
      this.command = server.command;
      this.plan = plans[this.id] || {};
      this.calls = { start: 0, stop: 0, dispose: 0 };
      this.state = 1;
      this.features = [];
      this.capabilities = {};
      this.clientOptions = { ...clientOptions, errorHandler: {
        error: () => ({ action: 1 }), closed: () => ({ action: 2 }),
      } };
      this.outputChannel = { dispose: () => h.resources.delete(this.id) };
      h.resources.add(this.id);
      if (options.record !== false) h.clients.push(this);
      record(`new ${id} ${server.command} ${server.args.join(' ')}`);
    }
    registerFeature(feature) { this.features.push(feature); }
    // Answers codeAction/resolve like the server: h.resolveReply(params) returns the reply or a promise of it.
    sendRequest(method, params) {
      h.requests.push([method, params]);
      return Promise.resolve(h.resolveReply ? h.resolveReply(params) : undefined);
    }
    async start() {
      for (const feature of this.features) feature.fillClientCapabilities?.(this.capabilities);
      this.calls.start++;
      record('start');
      // A failed/pending start can already own resources and a server.
      h.running.add(this.id);
      h.maxRunning = Math.max(h.maxRunning, h.running.size);
      this.plan.started?.resolve();
      if (this.plan.startGate) await this.plan.startGate.promise;
      if (this.plan.startError) {
        this.state = 4;
        throw this.plan.startError;
      }
      this.state = 2;
    }
    async stop() {
      this.calls.stop++;
      record('stop');
      this.plan.stopping?.resolve();
      if (this.plan.stopGate) await this.plan.stopGate.promise;
      if (this.plan.stopError && (!this.plan.stopOnce || this.calls.stop === 1)) throw this.plan.stopError;
      await this.shutdown();
    }
    async dispose() {
      this.calls.dispose++;
      record('dispose');
      this.plan.disposing?.resolve();
      if (this.plan.disposeGate) await this.plan.disposeGate.promise;
      if (this.plan.disposeError && (!this.plan.disposeOnce || this.calls.dispose === 1)) throw this.plan.disposeError;
      await this.shutdown();
    }
    async shutdown() {
      if (this.state === 4) throw new Error("Client is not running and can't be stopped. It's current state is: startFailed");
      for (const feature of this.features) feature.clear?.();
      h.running.delete(this.id);
      h.resources.delete(this.id);
      this.state = 1;
    }
    async handleConnectionClosed() {
      const result = await this.clientOptions.errorHandler.closed();
      if (result.action !== 1) throw new Error('failed client must not auto-restart');
      for (const feature of this.features) feature.clear?.();
      h.running.delete(this.id);
      h.resources.delete(this.id);
      this.state = 1;
    }
  }
  const extensionPath = path.resolve(options.extension || process.env.PEGO_EXTENSION || path.join(__dirname, '..', 'extension.js'));
  const load = Module._load;
  Module._load = function (request, ...rest) {
    if (request === 'vscode') return vscode;
    if (request === 'vscode-languageclient/node') return {
      LanguageClient, State: { Stopped: 1, Running: 2, Starting: 3, StartFailed: 4 },
      CloseAction: { DoNotRestart: 1, Restart: 2 },
    };
    return load.call(this, request, ...rest);
  };
  try {
    delete require.cache[extensionPath];
    h.extension = require(extensionPath);
  } finally {
    Module._load = load;
  }
  h.workspace = vscode.workspace;
  h.activate = () => h.extension.activate({ subscriptions: [] });
  h.restart = () => h.commands['pego.restartServer']();
  h.change = (affected = true) => h.configuration({ affectsConfiguration: () => affected });
  return h;
}

module.exports = { deferred, plan, within, loadExtension };
