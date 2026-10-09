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
    created: 0, maxRunning: 0,
  };
  const record = (event) => { if (options.record !== false) h.events.push(event); };
  const vscode = {
    workspace: {
      getConfiguration: () => ({ get: (key, fallback) => key in h.settings ? h.settings[key] : fallback }),
      onDidChangeConfiguration: (f) => { h.configuration = f; return { dispose() {} }; },
    },
    commands: {
      registerCommand: (name, f) => { h.commands[name] = f; return { dispose() {} }; },
    },
    window: {
      showErrorMessage: (message) => { record('error: ' + message); return Promise.resolve(); },
      createOutputChannel: () => ({ dispose() {} }),
    },
  };
  class LanguageClient {
    constructor(id, name, server) {
      this.id = h.created++;
      this.command = server.command;
      this.plan = plans[this.id] || {};
      this.calls = { start: 0, stop: 0, dispose: 0 };
      this.state = 1;
      this.clientOptions = { errorHandler: {
        error: () => ({ action: 1 }), closed: () => ({ action: 2 }),
      } };
      this.outputChannel = { dispose: () => h.resources.delete(this.id) };
      h.resources.add(this.id);
      if (options.record !== false) h.clients.push(this);
      record(`new ${id} ${server.command} ${server.args.join(' ')}`);
    }
    async start() {
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
      h.running.delete(this.id);
      h.resources.delete(this.id);
      this.state = 1;
    }
    async handleConnectionClosed() {
      const result = await this.clientOptions.errorHandler.closed();
      if (result.action !== 1) throw new Error('failed client must not auto-restart');
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
  h.activate = () => h.extension.activate({ subscriptions: [] });
  h.restart = () => h.commands['pego.restartServer']();
  h.change = (affected = true) => h.configuration({ affectsConfiguration: () => affected });
  return h;
}

module.exports = { deferred, plan, within, loadExtension };
