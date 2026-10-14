// Isolated client capability/action checks using the actual extension and host
// stand-ins. No editor window, server process or performance claims.
'use strict';

const assert = require('node:assert/strict');
const { loadExtension, deferred, within } = require('./extension-harness');
const manifest = require('../package.json');

const uri = { toString: () => 'file:///lint.pego' };
const document = () => ({ uri, version: 1, isClosed: false });
const token = () => ({ isCancellationRequested: false });
const diagnostic = (version = 1, generation = 1) => ({
  source: 'pego-lint', code: 'unused-capture', message: 'capture b is unused',
  data: { uri: uri.toString(), version, generation, key: '0:unused-capture' },
});
const explanation = (d = diagnostic()) => ({
  title: 'Explain unused-capture', diagnostics: [d],
  command: { command: 'pego.showExplanation', arguments: ['unused-capture', d.message, 'A discarded capture.'] },
});
const suppression = (d = diagnostic(), target = uri, text = '// lint:ignore unused-capture\n') => ({
  title: 'Suppress unused-capture on this finding line', diagnostics: [d],
  edit: { entries: () => [[target, [{ range: { start: { line: 1, character: 0 }, end: { line: 1, character: 0 } }, newText: text }]]] },
});
const publish = (h, d) => h.clients.at(-1).clientOptions.middleware.handleDiagnostics(uri, d, () => {});
const actions = (h, doc, next, cancellation = token()) =>
  h.clients.at(-1).clientOptions.middleware.provideCodeActions(doc, {}, {}, cancellation, next);
const execute = (h, action) => h.commands[action.command.command](...action.command.arguments);

async function using(enabled, f) {
  const h = loadExtension();
  h.settings['server.experimentalLint'] = enabled;
  await h.activate();
  try { await f(h); } finally { await h.extension.deactivate(); }
}

async function main() {
  assert.equal(manifest.contributes.configuration.properties['pego.server.experimentalLint'].default, false);
  await using(false, async (h) => {
    assert.equal(h.clients[0].capabilities.experimental, undefined);
    const d = document(); h.workspace.textDocuments = [d];
    const returned = [explanation()];
    assert.equal(await actions(h, d, () => returned), returned, 'disabled middleware passes ordinary actions through');
    assert.equal(await h.commands['pego.showExplanation']('unused-capture', 'message', 'explanation'), false);
    assert.equal(h.information.length, 0);
  });
  await using(true, async (h) => {
    const capabilities = h.clients[0].capabilities;
    assert.deepEqual(capabilities.experimental.pego, { lintDiagnostics: true, responsiveAnalysis: true, showExplanation: true });
    const extra = { experimental: { elsewhere: true, pego: { future: true } } };
    h.clients[0].features[0].fillClientCapabilities(extra);
    assert.equal(extra.experimental.elsewhere, true);
    assert.equal(extra.experimental.pego.future, true);
    const d = document(); h.workspace.textDocuments = [d]; publish(h, [diagnostic()]);
    const [explain] = await actions(h, d, () => [explanation(), suppression()]);
    assert.equal(await execute(h, explain), true);
    assert.equal(h.information.length, 1);
    assert.deepEqual(await actions(h, d, () => [suppression()]), [], 'unversioned WorkspaceEdit is not offered');
    assert.equal(await h.commands['pego.applyLintSuppression']({}, {}), false, 'legacy suppression commands are refused');
    assert.equal(h.applied.length, 0);
    d.version = 2;
    assert.equal(await execute(h, explain), false, 'reject stale explanation');
    assert.equal(h.applied.length, 0);
  });
  await using(true, async (h) => {
    const d = document(); h.workspace.textDocuments = [d]; publish(h, [diagnostic()]);
    const gate = deferred();
    const pending = actions(h, d, () => gate.promise);
    d.version = 2; gate.resolve([explanation()]);
    assert.deepEqual(await pending, [], 'discard provider response after editing');
    d.version = 1; publish(h, [diagnostic()]);
    const [old] = await actions(h, d, () => [explanation()]);
    publish(h, [diagnostic(1, 2)]);
    assert.equal(await execute(h, old), false, 'same-version save advances generation');
    assert.deepEqual(await actions(h, d, () => [explanation()]), []);
    publish(h, [diagnostic()]);
    const [closing] = await actions(h, d, () => [explanation()]);
    h.closedDocument(d);
    assert.equal(await execute(h, closing), false, 'close invalidates actions');
    publish(h, [diagnostic()]);
    const [restart] = await actions(h, d, () => [explanation()]);
    await h.restart(); publish(h, [diagnostic()]);
    assert.equal(await execute(h, restart), false, 'server generation reset cannot revive an old client action');
  });
  await using(true, async (h) => {
    const d = document(); h.workspace.textDocuments = [d]; publish(h, [diagnostic()]);
    const cancel = token(); cancel.isCancellationRequested = true;
    assert.deepEqual(await actions(h, d, () => [explanation()], cancel), []);
    assert.deepEqual(await actions(h, d, () => [explanation({ ...diagnostic(), data: undefined })]), []);
    assert.deepEqual(await actions(h, d, () => [{ ...explanation(), command: { command: 'unrelated.command' } }]), []);
    assert.deepEqual(await actions(h, d, () => [suppression(diagnostic(), { toString: () => 'file:///other.pego' })]), []);
    assert.deepEqual(await actions(h, d, () => [suppression(diagnostic(), uri, 'delete grammar source')]), []);
    const [explain] = await actions(h, d, () => [explanation()]);
    assert.equal(await h.commands['pego.showExplanation']('bad check', 'message', 'explanation', explain.command.arguments[3]), false);
    assert.equal(h.applied.length, 0);
  });
  await using(true, async (h) => {
    const d = document(); h.workspace.textDocuments = [d]; publish(h, [diagnostic()]);
    const pendingApply = deferred();
    let applyCalls = 0;
    h.workspace.applyEdit = () => { applyCalls++; return pendingApply.promise; };
    const candidates = [
      ['another valid line', suppression()],
      ['another valid check', suppression(diagnostic(), uri, '// lint:ignore shadowed-alternative\n')],
      ['another valid newline', suppression(diagnostic(), uri, '// lint:ignore unused-capture\r\n')],
      ['malformed range', { ...suppression(), edit: { entries: () => [[uri, [{ newText: '// lint:ignore unused-capture\n' }]]] } }],
      ['extra payload operation', { ...suppression(), edit: { entries: () => [[uri, [{ range: {}, newText: '// lint:ignore unused-capture\n' }]]] } }],
      ['unknown token', suppression()],
    ];
    for (const [name, candidate] of candidates) {
      assert.deepEqual(await actions(h, d, () => [candidate]), [], name + ': unsafe edit is not surfaced');
    }
    assert.equal(h.applied.length, 0, 'tampered actions never reach the editor');
    assert.equal(applyCalls, 0, 'no edit enters VS Code asynchronous application');
    const legacyInvocation = h.commands['pego.applyLintSuppression']({}, {});
    d.version = 2;
    pendingApply.resolve(true);
    assert.equal(await legacyInvocation, false, 'a document change during async command completion cannot apply a stale suppression');
    assert.equal(h.applied.length, 0, 'document changes during a pending apply cannot receive a stale suppression');
    assert.equal(applyCalls, 0);
  });
  await using(false, async (h) => {
    h.settings['server.experimentalLint'] = true;
    await h.change();
    assert.equal(h.clients.at(-1).capabilities.experimental.pego.lintDiagnostics, true);
    h.settings['server.experimentalLint'] = false;
    await h.change();
    assert.equal(h.clients.at(-1).capabilities.experimental, undefined);
  });
  console.log('lint capabilities and guarded actions ok');
}

within(main()).catch((error) => { console.error(error.stack || error); process.exitCode = 1; });
