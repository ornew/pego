// Optional lint UI for the responsive language-server prototype. The standard
// language client remains responsible for protocol conversion and cancellation.
'use strict';

const { randomUUID } = require('node:crypto');

function createLintActions(vscode, enabled, isCurrentClient) {
  const latest = new Map();
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
  };
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
    clear() { latest.clear(); },
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
          // The VS Code WorkspaceEdit conversion drops the LSP document
          // version. workspace.applyEdit has no conditional version parameter,
          // so a version check here cannot prevent edits racing with apply.
          return [];
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
    // Kept as a rejecting handler for stale commands from an already-open menu.
    // VS Code's WorkspaceEdit API cannot atomically enforce the LSP document
    // version, so lint suppression edits are not applied automatically.
    async applySuppression() { return false; },
  };
}

module.exports = { createLintActions };
