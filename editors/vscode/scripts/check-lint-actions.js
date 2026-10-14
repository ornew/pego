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
    const [explain, suppress] = await actions(h, d, () => [explanation(), suppression()]);
    assert.equal(await execute(h, explain), true);
    assert.equal(h.information.length, 1);
    assert.equal(suppress.edit, undefined, 'guarded edit is not auto-applied');
    const serialized = JSON.parse(JSON.stringify(suppress.command.arguments));
    assert.equal(await h.commands['pego.applyLintSuppression'](...serialized), true, 'plain command arguments survive editor RPC');
    assert.equal(h.applied.length, 1);
    assert.equal(h.applied[0].entries()[0][1][0].newText, '// lint:ignore unused-capture\n');
    d.version = 2;
    assert.equal(await execute(h, suppress), false, 'reject stale version at invocation');
    assert.equal(await execute(h, explain), false, 'reject stale explanation');
    assert.equal(h.applied.length, 1);
  });
  await using(true, async (h) => {
    const d = document(); h.workspace.textDocuments = [d]; publish(h, [diagnostic()]);
    const gate = deferred();
    const pending = actions(h, d, () => gate.promise);
    d.version = 2; gate.resolve([explanation()]);
    assert.deepEqual(await pending, [], 'discard provider response after editing');
    d.version = 1; publish(h, [diagnostic()]);
    const [old] = await actions(h, d, () => [suppression()]);
    publish(h, [diagnostic(1, 2)]);
    assert.equal(await execute(h, old), false, 'same-version save advances generation');
    assert.deepEqual(await actions(h, d, () => [explanation()]), []);
    publish(h, [diagnostic()]);
    const [closing] = await actions(h, d, () => [explanation()]);
    h.closedDocument(d);
    assert.equal(await execute(h, closing), false, 'close invalidates actions');
    publish(h, [diagnostic()]);
    const [restart] = await actions(h, d, () => [suppression()]);
    await h.restart(); publish(h, [diagnostic()]);
    assert.equal(await execute(h, restart), false, 'server generation reset cannot revive an old client action');
  });
  await using(true, async (h) => {
    const d = document(); h.workspace.textDocuments = [d]; publish(h, [diagnostic()]);
    const cancel = token(); cancel.isCancellationRequested = true;
    assert.deepEqual(await actions(h, d, () => [explanation()], cancel), []);
    assert.deepEqual(await actions(h, d, () => [explanation({ ...diagnostic(), data: undefined })]), []);
    assert.deepEqual(await actions(h, d, () => [{ ...explanation(), command: { command: 'unrelated.command' } }]), []);
    const [otherDocument] = await actions(h, d, () => [suppression(diagnostic(), { toString: () => 'file:///other.pego' })]);
    assert.equal(await execute(h, otherDocument), false);
    const [unsafeEdit] = await actions(h, d, () => [suppression(diagnostic(), uri, 'delete grammar source')]);
    assert.equal(await execute(h, unsafeEdit), false, 'only reviewed suppression insertions are applied');
    const [explain] = await actions(h, d, () => [explanation()]);
    assert.equal(await h.commands['pego.showExplanation']('bad check', 'message', 'explanation', explain.command.arguments[3]), false);
    assert.equal(h.applied.length, 0);
  });
  await using(true, async (h) => {
    const d = document(); h.workspace.textDocuments = [d]; publish(h, [diagnostic()]);
    const mutations = [
      ['another valid line', (edit) => { edit.start.line = 0; edit.end.line = 0; }],
      ['another valid check', (edit) => { edit.check = 'shadowed-alternative'; edit.newText = '// lint:ignore shadowed-alternative\n'; }],
      ['another valid newline', (edit) => { edit.newText = '// lint:ignore unused-capture\r\n'; }],
      ['malformed range', (edit) => { edit.start = null; }],
      ['extra payload operation', (edit) => { edit.fileOperation = 'delete'; }],
      ['unknown token', (edit) => { edit.token = 'not-issued'; }],
    ];
    for (const [name, mutate] of mutations) {
      const [approved] = await actions(h, d, () => [suppression()]);
      const original = JSON.parse(JSON.stringify(approved.command.arguments));
      const modified = JSON.parse(JSON.stringify(original));
      mutate(modified[0]);
      assert.equal(await h.commands['pego.applyLintSuppression'](...modified), false, name);
      if (name !== 'unknown token') {
        assert.equal(await h.commands['pego.applyLintSuppression'](...original), false, 'failed attempt consumes its token');
      }
    }
    assert.equal(h.applied.length, 0, 'tampered actions never reach the editor');
    const [mutable] = await actions(h, d, () => [suppression()]);
    mutable.command.arguments[0].start.line = 0;
    mutable.command.arguments[0].end.line = 0;
    assert.equal(await execute(h, mutable), false, 'approval is an independent immutable copy');
    const [once] = await actions(h, d, () => [suppression()]);
    const valid = JSON.parse(JSON.stringify(once.command.arguments));
    const first = h.commands['pego.applyLintSuppression'](...valid);
    assert.equal(await h.commands['pego.applyLintSuppression'](...valid), false, 'concurrent replay rejected');
    assert.equal(await first, true);
    assert.equal(await h.commands['pego.applyLintSuppression'](...valid), false, 'completed replay rejected');
    assert.equal(h.applied.length, 1);
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
