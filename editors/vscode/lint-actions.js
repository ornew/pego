// Optional lint UI for the responsive language-server prototype. The standard
// language client remains responsible for protocol conversion and cancellation.
'use strict';

const { randomUUID } = require('node:crypto');

function createLintActions(vscode, enabled, isCurrentClient) {
  const latest = new Map();
  const issuedSuppressions = new Map();
  const session = randomUUID();
  const uriText = (uri) => uri.toString();
  const documentFor = (uri) => vscode.workspace.textDocuments.find((d) => !d.isClosed && uriText(d.uri) === uri);
  const active = () => enabled && isCurrentClient();
  const findingData = (diagnostic) => {
    const data = diagnostic.data;
    return diagnostic.source === 'pego-lint' && data && typeof data.uri === 'string' &&
      Number.isInteger(data.version) && Number.isSafeInteger(data.generation) && data.generation > 0 &&
      typeof data.key === 'string' ? data : undefined;
  };
  const findingCurrent = (guard) => {
    if (!active() || !guard || typeof guard.uri !== 'string') return false;
    const document = documentFor(guard.uri), current = latest.get(guard.uri);
    return document && document.version === guard.version && current &&
      current.version === guard.version && current.generation === guard.generation;
  };
  const guardCurrent = (guard) => guard && guard.session === session && findingCurrent(guard);
  const forgetDocument = (uri) => {
    latest.delete(uri);
    for (const [token, approval] of issuedSuppressions) {
      if (approval.guard.uri === uri) issuedSuppressions.delete(token);
    }
  };
  const exactKeys = (value, keys) => value && typeof value === 'object' &&
    Object.keys(value).sort().join(',') === keys;
  const samePayload = (edit, approved) => exactKeys(edit, 'check,end,newText,start,token,uri') &&
    exactKeys(edit.start, 'character,line') && exactKeys(edit.end, 'character,line') &&
    edit.token === approved.token && edit.uri === approved.uri && edit.check === approved.check &&
    edit.newText === approved.newText && edit.start.line === approved.start.line &&
    edit.start.character === approved.start.character && edit.end.line === approved.end.line &&
    edit.end.character === approved.end.character;
  const sameGuard = (guard, approved) => exactKeys(guard, 'generation,session,uri,version') &&
    guard.uri === approved.uri && guard.version === approved.version &&
    guard.generation === approved.generation && guard.session === approved.session;
  const feature = {
    fillClientCapabilities(capabilities) {
      if (!enabled) return;
      capabilities.experimental = capabilities.experimental || {};
      capabilities.experimental.pego = {
        ...capabilities.experimental.pego,
        lintDiagnostics: true, responsiveAnalysis: true, showExplanation: true,
      };
    },
    initialize() {},
    getState() { return { kind: 'static' }; },
    clear() { latest.clear(); issuedSuppressions.clear(); },
  };
  const middleware = {
    handleDiagnostics(uri, diagnostics, next) {
      if (enabled) {
        const key = uriText(uri);
        forgetDocument(key);
        for (const diagnostic of diagnostics) {
          const data = findingData(diagnostic);
          if (data && data.uri === key) latest.set(key, { version: data.version, generation: data.generation });
        }
      }
      return next(uri, diagnostics);
    },
    async provideCodeActions(document, range, context, token, next) {
      if (!enabled) return next(document, range, context, token);
      const version = document.version, uri = uriText(document.uri);
      const actions = await next(document, range, context, token);
      if (!active() || token.isCancellationRequested || document.isClosed ||
          documentFor(uri) !== document || document.version !== version) return [];
      return (actions || []).flatMap((action) => {
        const diagnostics = action.diagnostics || [];
        const findings = diagnostics.filter((d) => d.source === 'pego-lint');
        if (findings.length === 0) return action.command?.command === 'pego.showExplanation' ? [] : [action];
        const data = findings.map(findingData);
        if (data.some((d) => !d || d.uri !== uri || d.version !== version || !findingCurrent(d))) return [];
        const guard = { uri, version, generation: data[0].generation, session };
        if (data.some((d) => d.generation !== guard.generation)) return [];
        if (action.command) {
          if (action.command.command !== 'pego.showExplanation' || action.edit ||
              !Array.isArray(action.command.arguments) || action.command.arguments.length !== 3) return [];
          action.command = { ...action.command, arguments: [...(action.command.arguments || []), guard] };
        } else if (action.edit) {
          // VS Code's converted WorkspaceEdit does not carry the LSP document
          // identifier. Guard again at invocation rather than auto-applying it.
          const entries = action.edit.entries();
          if (entries.length !== 1 || entries[0][1].length !== 1) return [];
          const edit = entries[0][1][0];
          if (!edit.range) return [];
          const check = typeof findings[0].code === 'string' ? findings[0].code : findings[0].code?.value;
          const payload = { token: randomUUID(), check, uri: uriText(entries[0][0]), newText: edit.newText,
            start: { line: edit.range.start.line, character: edit.range.start.character },
            end: { line: edit.range.end.line, character: edit.range.end.character } };
          // Hold an independent approved copy: command arguments are editable
          // plain data and must not become a general-purpose insertion API.
          issuedSuppressions.set(payload.token, {
            guard: Object.freeze({ ...guard }),
            payload: Object.freeze({ ...payload,
              start: Object.freeze({ ...payload.start }), end: Object.freeze({ ...payload.end }) }),
          });
          // Bound retained approvals. Evicted menus can request fresh actions.
          while (issuedSuppressions.size > 256) issuedSuppressions.delete(issuedSuppressions.keys().next().value);
          // Command arguments cross the editor RPC boundary: send plain data,
          // never a WorkspaceEdit instance whose prototype would be lost.
          action.command = { title: action.title, command: 'pego.applyLintSuppression', arguments: [payload, guard] };
          action.edit = undefined;
        } else {
          return [];
        }
        return [action];
      });
    },
  };
  return {
    feature, middleware,
    closeDocument(document) { forgetDocument(uriText(document.uri)); },
    async showExplanation(check, message, explanation, guard) {
      if (!guardCurrent(guard) || typeof check !== 'string' || !/^[a-z][a-z0-9-]{0,99}$/.test(check) ||
          typeof message !== 'string' || typeof explanation !== 'string') return false;
      await vscode.window.showInformationMessage(`PEGO ${check}: ${message}\n${explanation}`);
      return true;
    },
    async applySuppression(edit, guard) {
      if (!edit || typeof edit.token !== 'string') return false;
      const approval = issuedSuppressions.get(edit.token);
      if (!approval) return false;
      // Any invocation consumes the token, even a malformed or stale attempt.
      issuedSuppressions.delete(edit.token);
      if (!samePayload(edit, approval.payload) || !sameGuard(guard, approval.guard) ||
          !guardCurrent(guard) || edit.uri !== guard.uri) return false;
      if (typeof edit.check !== 'string' || !/^[a-z][a-z0-9-]*$/.test(edit.check) ||
          typeof edit.newText !== 'string') return false;
      const directive = `// lint:ignore ${edit.check}`;
      if (![directive + '\n', directive + '\r\n', directive + '\r'].includes(edit.newText)) return false;
      if (!Number.isInteger(edit.start.line) || edit.start.line < 0 || edit.start.character !== 0 ||
          edit.start.line !== edit.end.line || edit.start.character !== edit.end.character) return false;
      // Reconstruct only the reviewed single-document insertion, so a command
      // payload cannot carry file operations or unrelated edits.
      const safeEdit = new vscode.WorkspaceEdit();
      safeEdit.insert(vscode.Uri.parse(edit.uri), new vscode.Position(edit.start.line, 0), edit.newText);
      return vscode.workspace.applyEdit(safeEdit);
    },
  };
}

module.exports = { createLintActions };
