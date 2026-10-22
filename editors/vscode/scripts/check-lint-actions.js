// Isolated client capability/action checks using the actual extension and host
// stand-ins. No editor window, server process or performance claims.
'use strict';

const assert = require('node:assert/strict');
const { loadExtension, deferred, within } = require('./extension-harness');
const manifest = require('../package.json');

const uri = { toString: () => 'file:///lint.pego' };
const document = () => ({ uri, version: 1, isClosed: false, lineCount: 4 });
const token = () => ({ isCancellationRequested: false });
const diagnostic = (version = 1, generation = 1, code = 'unused-capture') => ({
  source: 'pego-lint', code, message: 'capture b is unused',
  data: { uri: uri.toString(), version, generation, key: '0:unused-capture' },
});
const explanation = (d = diagnostic()) => ({
  title: 'Explain unused-capture', diagnostics: [d],
  command: { command: 'pego.showExplanation', arguments: ['unused-capture', d.message, 'A discarded capture.'] },
});
const suppression = (d = diagnostic(), target = uri, text = '// lint:ignore unused-capture\n', line = 1, character = 0) => ({
  title: 'Suppress unused-capture on this finding line', diagnostics: [d],
  edit: { entries: () => [[target, [{ range: { start: { line, character }, end: { line, character } }, newText: text }]]] },
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
    const ordinary = { title: 'ordinary', diagnostics: [{ source: 'other' }] };
    assert.deepEqual(await actions(h, d, () => [ordinary]), [ordinary], 'disabled middleware passes ordinary actions through');
    assert.deepEqual(await actions(h, d, () => returned), [], 'disabled middleware drops lint actions');
    assert.deepEqual(await actions(h, d, () => [suppression()]), [], 'disabled middleware drops lint edits');
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
    const [suppress] = await actions(h, d, () => [suppression()]);
    assert.equal(suppress.edit, undefined, 'the unversioned WorkspaceEdit is replaced by a guarded command');
    assert.equal(suppress.command.command, 'pego.applyLintSuppression');
    assert.deepEqual(suppress.command.arguments.slice(0, 2), [1, '// lint:ignore unused-capture\n']);
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
    const unsafe = [
      ['wrong check text', suppression(diagnostic(), uri, '// lint:ignore shadowed-alternative\n')],
      ['text for another check', suppression(diagnostic(1, 1, 'shadowed-alternative'))],
      ['not a directive', suppression(diagnostic(), uri, 'delete grammar source')],
      ['directive without line break', suppression(diagnostic(), uri, '// lint:ignore unused-capture')],
      ['directive with trailing text', suppression(diagnostic(), uri, '// lint:ignore unused-capture\nrule x = "y"\n')],
      ['mid-line position', suppression(diagnostic(), uri, undefined, 1, 3)],
      ['negative line', suppression(diagnostic(), uri, undefined, -1)],
      ['malformed range', { ...suppression(), edit: { entries: () => [[uri, [{ newText: '// lint:ignore unused-capture\n' }]]] } }],
      ['empty range object', { ...suppression(), edit: { entries: () => [[uri, [{ range: {}, newText: '// lint:ignore unused-capture\n' }]]] } }],
      ['spanning range', { ...suppression(), edit: { entries: () => [[uri, [{ range: { start: { line: 1, character: 0 }, end: { line: 2, character: 0 } }, newText: '// lint:ignore unused-capture\n' }]]] } }],
      ['two edits', { ...suppression(), edit: { entries: () => [[uri, [
        { range: { start: { line: 1, character: 0 }, end: { line: 1, character: 0 } }, newText: '// lint:ignore unused-capture\n' },
        { range: { start: { line: 2, character: 0 }, end: { line: 2, character: 0 } }, newText: '// lint:ignore unused-capture\n' }]]] } }],
      ['two documents', { ...suppression(), edit: { entries: () => [...suppression().edit.entries(), ...suppression(diagnostic(), { toString: () => 'file:///other.pego' }).edit.entries()] } }],
      ['two findings', { ...suppression(), diagnostics: [diagnostic(), diagnostic()] }],
      ['edit and command', { ...suppression(), command: { command: 'pego.showExplanation', arguments: ['unused-capture', 'message', 'explanation'] } }],
    ];
    for (const [name, candidate] of unsafe) {
      assert.deepEqual(await actions(h, d, () => [candidate]), [], name + ': unsafe edit is not surfaced');
    }
    assert.equal(h.applied.length, 0, 'tampered actions never reach the editor');
    for (const text of ['// lint:ignore unused-capture\r\n', '// lint:ignore unused-capture\r']) {
      const [ok] = await actions(h, d, () => [suppression(diagnostic(), uri, text)]);
      assert.equal(ok.command.arguments[1], text, 'the document newline convention is kept');
    }
  });
  await using(true, async (h) => {
    const d = document(); h.workspace.textDocuments = [d]; publish(h, [diagnostic()]);
    h.mainVersion = () => d.version;
    const [action] = await actions(h, d, () => [suppression()]);
    const guard = action.command.arguments[2];
    // The edit enters applyEdit during the command call itself, stamped with the
    // analyzed version, before any await can interleave a document change.
    const pending = execute(h, action);
    assert.equal(h.applied.length, 1, 'applyEdit is called synchronously');
    assert.deepEqual(h.appliedVersions[0], [guard.version]);
    const [[target, edits]] = h.applied[0].entries();
    assert.equal(target, uri);
    assert.deepEqual(edits.map((e) => [e.range.start.line, e.range.start.character, e.newText]), [[1, 0, '// lint:ignore unused-capture\n']]);
    assert.equal(await pending, true);
    // Replaying a command after an edit is refused before reaching the editor.
    d.version = 2;
    assert.equal(await execute(h, action), false, 'stale version is not applied');
    assert.equal(await h.commands['pego.applyLintSuppression'](1, '// lint:ignore unused-capture\n', { ...guard, version: 2 }), false, 'a forged guard is refused');
    assert.equal(await h.commands['pego.applyLintSuppression'](1, '// lint:ignore unused-capture\n', { ...guard, session: 'x' }), false, 'a guard from another session is refused');
    assert.equal(h.applied.length, 1);
    // A change the extension host has not seen yet is refused by the main thread.
    d.version = 1; publish(h, [diagnostic()]);
    const [again] = await actions(h, d, () => [suppression()]);
    h.mainVersion = () => 2;
    assert.equal(await execute(h, again), false, 'main thread refuses an edit stamped with an older version');
    assert.equal(h.applied.length, 2);
    // Arguments are validated again at execution time.
    h.mainVersion = () => d.version;
    const args = again.command.arguments;
    for (const bad of [[-1, args[1]], [4, args[1]], [1.5, args[1]], [1, 'delete everything\n'], [1, '// lint:ignore a\nrule x = "y"\n'], [1, undefined]]) {
      assert.equal(await h.commands['pego.applyLintSuppression'](...bad, args[2]), false, JSON.stringify(bad));
    }
    assert.equal(h.applied.length, 2);
    // A new publication or a close invalidates a command already in a menu.
    publish(h, [diagnostic(1, 2)]);
    assert.equal(await execute(h, again), false, 'same-version publication advances generation');
    publish(h, [diagnostic()]);
    const [closing] = await actions(h, d, () => [suppression()]);
    h.closedDocument(d);
    assert.equal(await execute(h, closing), false, 'close invalidates the command');
    assert.equal(h.applied.length, 2);
  });
  await using(false, async (h) => {
    const d = document(); h.workspace.textDocuments = [d];
    assert.equal(await h.commands['pego.applyLintSuppression'](1, '// lint:ignore unused-capture\n', {}), false, 'disabled lint applies nothing');
    assert.equal(h.applied.length, 0);
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
