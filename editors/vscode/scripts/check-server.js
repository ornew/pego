// Talks to the language server with the JSON-RPC library of the VS Code language client, as the
// extension does, to check that the two interoperate: initialize, diagnostics after an edit,
// hover, completion, formatting, and a clean shutdown.
//
// Usage: node scripts/check-server.js [pego command]   (default: pego on the PATH)
'use strict';

const { spawn } = require('child_process');
const rpc = require('vscode-jsonrpc/node');

const command = process.argv[2] || 'pego';
const uri = 'file:///check/calc.pego';
const text = 'def main = "😀" num\ndef num = (?0-9)+\n';

function fail(message) {
  console.error('FAIL: ' + message);
  process.exitCode = 1;
}

async function main() {
  const server = spawn(command, ['lsp'], { stdio: ['pipe', 'pipe', 'inherit'] });
  const exited = new Promise((resolve) => server.on('exit', (code) => resolve(code)));
  const conn = rpc.createMessageConnection(
    new rpc.StreamMessageReader(server.stdout),
    new rpc.StreamMessageWriter(server.stdin),
  );
  const diagnostics = [];
  let notify;
  conn.onNotification('textDocument/publishDiagnostics', (p) => {
    diagnostics.push(p);
    if (notify) notify();
  });
  conn.listen();
  const nextDiagnostics = () =>
    new Promise((resolve) => {
      notify = () => resolve(diagnostics.shift());
      if (diagnostics.length > 0) notify();
    });

  const init = await conn.sendRequest('initialize', { processId: process.pid, rootUri: null, capabilities: {} });
  if (init.capabilities.positionEncoding !== 'utf-16' || !init.capabilities.hoverProvider) {
    fail('initialize: ' + JSON.stringify(init.capabilities));
  }
  conn.sendNotification('initialized', {});

  conn.sendNotification('textDocument/didOpen', { textDocument: { uri, languageId: 'pego', version: 1, text } });
  let d = await nextDiagnostics();
  if (d.diagnostics.length !== 0) fail('diagnostics of a valid grammar: ' + JSON.stringify(d));

  // Rename the reference to an undefined rule, after the astral character (two UTF-16 units).
  conn.sendNotification('textDocument/didChange', {
    textDocument: { uri, version: 2 },
    contentChanges: [{ range: { start: { line: 0, character: 16 }, end: { line: 0, character: 19 } }, text: 'nums' }],
  });
  d = await nextDiagnostics();
  const want = { start: { line: 0, character: 16 }, end: { line: 0, character: 20 } };
  if (d.version !== 2 || d.diagnostics.length !== 1 || JSON.stringify(d.diagnostics[0].range) !== JSON.stringify(want)) {
    fail('diagnostics after the change: ' + JSON.stringify(d));
  }
  conn.sendNotification('textDocument/didChange', {
    textDocument: { uri, version: 3 },
    contentChanges: [{ range: { start: { line: 0, character: 19 }, end: { line: 0, character: 20 } }, text: '' }],
  });
  await nextDiagnostics();

  const hover = await conn.sendRequest('textDocument/hover', { textDocument: { uri }, position: { line: 1, character: 5 } });
  if (!hover || !hover.contents.value.includes('def num: []Match')) fail('hover: ' + JSON.stringify(hover));

  const completion = await conn.sendRequest('textDocument/completion', {
    textDocument: { uri },
    position: { line: 0, character: 17 },
  });
  if (!completion.items.some((i) => i.label === 'num')) fail('completion: ' + JSON.stringify(completion));

  const edits = await conn.sendRequest('textDocument/formatting', {
    textDocument: { uri },
    options: { tabSize: 4, insertSpaces: true },
  });
  if (!Array.isArray(edits)) fail('formatting: ' + JSON.stringify(edits));

  await conn.sendRequest('shutdown');
  conn.sendNotification('exit');
  const code = await exited;
  if (code !== 0) fail(`the server exited with status ${code}`);
  conn.dispose();
  if (!process.exitCode) console.log('ok');
}

main().catch((err) => {
  fail(err.stack || String(err));
  process.exit(1);
});
