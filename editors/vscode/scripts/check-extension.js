// Checks the extension's manifest and its activation with stand-ins for the VS Code API and the
// language client: that a workspace cannot choose the command run as the server unless it is
// trusted, and that a client whose server fails to start is disposed of and can be restarted.
//
// Run with `npm run check` after `npm install`.
'use strict';

const Module = require('module');
const path = require('path');

let failures = 0;
function fail(message) {
  console.error('FAIL: ' + message);
  failures++;
}

// The manifest: pego.server.path names a program to run, so in an untrusted workspace VS Code must
// ignore the workspace's value of it.
const manifest = require(path.join(__dirname, '..', 'package.json'));
const serverPath = manifest.contributes.configuration.properties['pego.server.path'];
if (serverPath.scope !== 'machine-overridable') {
  fail(`pego.server.path has scope ${serverPath.scope}, want machine-overridable`);
}
const untrusted = (manifest.capabilities || {}).untrustedWorkspaces || {};
if (untrusted.supported !== 'limited' || !(untrusted.restrictedConfigurations || []).includes('pego.server.path')) {
  fail('capabilities.untrustedWorkspaces does not restrict pego.server.path: ' + JSON.stringify(untrusted));
}

// Stand-ins for the vscode module and the language client.
const events = [];
const settings = { 'server.enabled': true, 'server.path': '/no/such/pego' };
const commands = {};
let startFails = true;

const vscode = {
  workspace: {
    getConfiguration: () => ({ get: (key, def) => (key in settings ? settings[key] : def) }),
    onDidChangeConfiguration: () => ({ dispose() {} }),
  },
  commands: {
    registerCommand: (name, f) => {
      commands[name] = f;
      return { dispose() {} };
    },
  },
  window: { showErrorMessage: (m) => events.push('error: ' + m) },
};

class LanguageClient {
  constructor(id, name, server) {
    this.command = server.command;
    events.push(`new ${id} ${server.command} ${server.args.join(' ')}`);
  }
  async start() {
    events.push('start');
    if (startFails) {
      throw new Error('spawn ENOENT');
    }
  }
  async stop() {
    events.push('stop');
  }
  async dispose() {
    events.push('dispose');
  }
}

const load = Module._load;
Module._load = function (request, ...rest) {
  if (request === 'vscode') return vscode;
  if (request === 'vscode-languageclient/node') return { LanguageClient };
  return load.call(this, request, ...rest);
};
const extension = require('../extension.js');

async function main() {
  // The server cannot start: the error is shown and the client disposed of.
  await extension.activate({ subscriptions: [] });
  const want = ['new pego /no/such/pego lsp', 'start', 'dispose'];
  const got = events.filter((e) => !e.startsWith('error: '));
  if (JSON.stringify(got) !== JSON.stringify(want)) {
    fail(`failed start: events ${JSON.stringify(events)}, want ${JSON.stringify(want)} and an error`);
  }
  if (!events.some((e) => e.startsWith('error: ') && e.includes('/no/such/pego lsp'))) {
    fail('failed start: no error message: ' + JSON.stringify(events));
  }

  // Restarting after installing pego starts a new client; deactivating stops it.
  events.length = 0;
  startFails = false;
  await commands['pego.restartServer']();
  await extension.deactivate();
  if (JSON.stringify(events) !== JSON.stringify(['new pego /no/such/pego lsp', 'start', 'stop'])) {
    fail('restart: events ' + JSON.stringify(events));
  }

  // A disabled server is not started.
  events.length = 0;
  settings['server.enabled'] = false;
  await commands['pego.restartServer']();
  if (events.length !== 0) {
    fail('disabled server: events ' + JSON.stringify(events));
  }
}

main()
  .catch((err) => fail(err.stack || String(err)))
  .finally(() => {
    if (failures > 0) {
      console.error(`${failures} failure(s)`);
      process.exit(1);
    }
    console.log('extension ok');
  });
