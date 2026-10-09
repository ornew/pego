// Checks the extension's manifest and its activation with stand-ins for the VS Code API and the
// language client: trusted command configuration and serialized client ownership
// across overlapping restarts, configuration changes, failures and shutdown.
//
// Run with `npm run check` after `npm install`.
'use strict';

const assert = require('node:assert/strict');
const { deferred, plan, within, loadExtension } = require('./extension-harness');
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

async function check(name, f) {
  try {
    await within(f());
    console.log(name + ' ok');
  } catch (err) {
    fail(name + ': ' + (err.stack || err));
  }
}

function empty(h) {
  assert.equal(h.running.size, 0, 'no running client');
  assert.equal(h.resources.size, 0, 'no retained client resources');
}

async function main() {
  await check('failed start and retry', async () => {
    const h = loadExtension([plan({ startError: new Error('spawn ENOENT') })]);
    h.settings['server.path'] = '/no/such/pego';
    await h.activate();
    empty(h);
    assert.equal(h.clients[0].calls.dispose, 1);
    assert(h.events.some((e) => e.startsWith('error: ') && e.includes('/no/such/pego lsp')));
    await h.restart();
    assert.equal(h.running.size, 1);
    await h.extension.deactivate();
    empty(h);
  });

  await check('disabled startup and enable', async () => {
    const h = loadExtension();
    h.settings['server.enabled'] = false;
    await h.activate();
    await h.restart();
    assert.equal(h.created, 0);
    h.settings['server.enabled'] = true;
    await h.change();
    assert.equal(h.running.size, 1);
    await h.extension.deactivate();
    empty(h);
  });

  await check('overlapping restarts', async () => {
    const first = plan({ startGate: deferred() }), second = plan({ startGate: deferred() });
    const h = loadExtension([plan(), first, second]);
    await h.activate();
    const a = h.restart(), b = h.restart();
    await first.started.promise;
    first.startGate.resolve();
    await second.started.promise;
    second.startGate.resolve();
    await Promise.all([a, b]);
    assert.equal(h.maxRunning, 1, 'transitions must not overlap live clients');
    assert.equal(h.running.size, 1);
    await h.extension.deactivate();
    empty(h);
  });

  await check('restart waits for stop', async () => {
    const first = plan({ stopGate: deferred() });
    const h = loadExtension([first]);
    await h.activate();
    const a = h.restart(), b = h.restart();
    await first.stopping.promise;
    assert.equal(h.created, 1, 'no replacement before stop finishes');
    first.stopGate.resolve();
    await Promise.all([a, b]);
    assert.equal(h.maxRunning, 1);
    await h.extension.deactivate();
    empty(h);
  });

  await check('stop rejection preserves ownership', async () => {
    const err = new Error('stop failed');
    const h = loadExtension([plan({ stopError: err, stopOnce: true })]);
    await h.activate();
    await assert.rejects(h.restart(), (e) => e === err);
    assert.equal(h.created, 1, 'failed stop must not create a replacement');
    assert.equal(h.running.size, 1);
    await h.extension.deactivate();
    assert.equal(h.clients[0].calls.stop, 2, 'retry still owns the rejected client');
    empty(h);
  });

  await check('repeated cleanup failures do not poison the queue', async () => {
    const err = new Error('stop failed'), first = plan({ stopError: err });
    const h = loadExtension([first]);
    await h.activate();
    const a = h.restart(), b = h.restart();
    await Promise.all([assert.rejects(a, (e) => e === err), assert.rejects(b, (e) => e === err)]);
    assert.equal(h.created, 1);
    await assert.rejects(h.extension.deactivate(), (e) => e === err);
    assert.equal(h.running.size, 1);
    first.stopError = undefined;
    await h.extension.deactivate();
    empty(h);
    await h.restart();
    assert.equal(h.created, 1, 'cleanup retry does not reactivate');
  });

  await check('failed-start disposal waits and retries', async () => {
    const first = plan({ startError: new Error('start failed'), disposeGate: deferred(),
      disposeError: new Error('dispose failed'), disposeOnce: true });
    const h = loadExtension([first]);
    const activation = h.activate();
    // Observe rejection immediately, before opening the cleanup gate.
    const rejected = assert.rejects(activation, /dispose failed/);
    await first.disposing.promise;
    const restart = h.restart();
    assert.equal(h.created, 1, 'no replacement during failed-start disposal');
    first.disposeGate.resolve();
    await rejected;
    await restart;
    assert.equal(h.clients[0].calls.dispose, 2);
    assert.equal(h.running.size, 1);
    assert.equal(h.maxRunning, 1);
    await h.extension.deactivate();
    empty(h);
  });

  await check('configuration during pending start', async () => {
    const first = plan({ startGate: deferred() });
    const h = loadExtension([first]);
    const activation = h.activate();
    await first.started.promise;
    h.settings['server.enabled'] = false;
    const disabled = h.change();
    first.startGate.resolve();
    await Promise.all([activation, disabled]);
    empty(h);
    h.settings['server.enabled'] = true;
    h.settings['server.path'] = '/new/pego';
    await h.change();
    assert.equal(h.running.size, 1);
    assert.equal(h.clients.at(-1).command, '/new/pego');
    const count = h.created;
    await h.change(false);
    assert.equal(h.created, count, 'unrelated settings do not restart');
    await h.extension.deactivate();
    empty(h);
  });

  await check('configuration stop error is handled', async () => {
    const h = loadExtension([plan({ stopError: new Error('stop failed'), stopOnce: true })]);
    await h.activate();
    await h.change();
    assert(h.events.some((e) => e.startsWith('error: ') && e.includes('stop failed')));
    assert.equal(h.created, 1);
    await h.extension.deactivate();
    empty(h);
  });

  await check('overlapping path changes use current settings', async () => {
    const first = plan({ stopGate: deferred() });
    const h = loadExtension([first]);
    await h.activate();
    h.settings['server.path'] = '/old/pego';
    const a = h.change();
    await first.stopping.promise;
    h.settings['server.path'] = '/latest/pego';
    const b = h.change();
    first.stopGate.resolve();
    await Promise.all([a, b]);
    assert.equal(h.maxRunning, 1);
    assert(h.clients.slice(1).every((c) => c.command === '/latest/pego'));
    assert.equal(h.running.size, 1);
    await h.extension.deactivate();
    empty(h);
  });

  await check('deactivate waits for failed-start disposal', async () => {
    const first = plan({ startError: new Error('start failed'), disposeGate: deferred() });
    const h = loadExtension([first]);
    const activation = h.activate();
    await first.disposing.promise;
    const shutdown = h.extension.deactivate(), late = h.restart();
    first.disposeGate.resolve();
    await Promise.all([activation, shutdown, late]);
    empty(h);
    assert.equal(h.created, 1);
  });

  await check('deactivate during start and late restart', async () => {
    const first = plan({ startGate: deferred() });
    const h = loadExtension([first]);
    const activation = h.activate();
    await first.started.promise;
    const shutdown = h.extension.deactivate();
    const lateRestart = h.restart();
    first.startGate.resolve();
    await Promise.all([activation, shutdown, lateRestart]);
    empty(h);
    assert.equal(h.created, 1, 'late restart must not revive a deactivated extension');
    await h.change();
    empty(h);
  });

  await check('deactivate skips queued start', async () => {
    const h = loadExtension();
    const activation = h.activate();
    const shutdown = h.extension.deactivate();
    await Promise.all([activation, shutdown]);
    empty(h);
    assert.equal(h.created, 0);
  });
}

main().catch((err) => fail(err.stack || String(err))).finally(() => {
  if (failures) {
    console.error(`${failures} failure(s)`);
    process.exitCode = 1;
  } else {
    console.log('extension ok');
  }
});
