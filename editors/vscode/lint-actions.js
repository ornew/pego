// Optional lint UI for the responsive language-server prototype. The standard
// language client remains responsible for protocol conversion and cancellation.
'use strict';

const { randomUUID } = require('node:crypto');

function createLintActions(vscode, enabled, isCurrentClient, resolveRequest) {
  const latest = new Map();
  const session = randomUUID();
  const uriText = (uri) => uri.toString();
  const documentFor = (uri) => vscode.workspace.textDocuments.find((d) => !d.isClosed && uriText(d.uri) === uri);
  const active = () => enabled && isCurrentClient();
  const findingData = (diagnostic) => {
    const data = diagnostic.data;
    return diagnostic.source === 'zp-lint' && data && typeof data.uri === 'string' &&
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
  // A suppression is exactly one zero-width insertion of `// lint:ignore
  // <check>` plus a line break at the start of a line of this document, for the
  // check the single diagnostic reports. Anything else is not surfaced.
  const suppressionText = (check, text) =>
    typeof check === 'string' && /^[a-z][a-z0-9-]{0,99}$/.test(check) &&
    (text === `// lint:ignore ${check}\n` || text === `// lint:ignore ${check}\r\n` || text === `// lint:ignore ${check}\r`);
  const suppressionTarget = (edit, uri, findings) => {
    if (findings.length !== 1 || typeof edit.entries !== 'function') return undefined;
    const entries = edit.entries();
    if (!Array.isArray(entries) || entries.length !== 1) return undefined;
    const [target, edits] = entries[0];
    if (!target || uriText(target) !== uri || !Array.isArray(edits) || edits.length !== 1) return undefined;
    const { range, newText } = edits[0], code = findings[0].code;
    const check = code && typeof code === 'object' ? code.value : code;
    if (!range || !range.start || !range.end || !suppressionText(check, newText)) return undefined;
    const { start, end } = range;
    if (!Number.isSafeInteger(start.line) || start.line < 0 || start.character !== 0 ||
        end.line !== start.line || end.character !== 0) return undefined;
    return { line: start.line, text: newText };
  };
  // A fix replaces a span of one document for the single finding it belongs to.
  // The server proves each kind and verifies the edited grammar; the client
  // accepts only the shapes below, judged on the text the edit replaces.
  const fixShapes = {
    // Overlapping ranges become a merged class.
    'char-class': (old, text) => /^\[[^\r\n]*\]$/.test(old) && /^\[[^\r\n]*\]$/.test(text),
    // A lookahead that always succeeds is deleted.
    'useless-lookahead': (old, text) => text === '' && /^[&!][^\r\n]*$/.test(old),
    // An unreachable rule is deleted, with its leading attributes and comment lines.
    'unreachable-rule': (old, text) => text === '' && /(^|\n)[ \t]*rule[ \t]+[A-Za-z_]/.test(old) &&
      !old.includes('lint:file-ignore'),
  };
  const fixCheck = (check) => typeof check === 'string' && Object.hasOwn(fixShapes, check) ? check : undefined;
  const validPositions = (r) => r && r.start && r.end &&
    [r.start.line, r.start.character, r.end.line, r.end.character].every((n) => Number.isSafeInteger(n) && n >= 0);
  const validRange = (r) => validPositions(r) &&
    (r.end.line > r.start.line || r.end.line === r.start.line && r.end.character > r.start.character);
  const fixTarget = (edit, uri, findings, document) => {
    if (findings.length !== 1 || typeof edit.entries !== 'function' || typeof document.getText !== 'function') return undefined;
    const entries = edit.entries();
    if (!Array.isArray(entries) || entries.length !== 1) return undefined;
    const [target, edits] = entries[0];
    if (!target || uriText(target) !== uri || !Array.isArray(edits) || edits.length !== 1) return undefined;
    const { range, newText } = edits[0], code = findings[0].code;
    const check = fixCheck(code && typeof code === 'object' ? code.value : code);
    if (!check || !validRange(range) || typeof newText !== 'string') return undefined;
    const old = document.getText(range);
    if (typeof old !== 'string' || !fixShapes[check](old, newText)) return undefined;
    return { check, range: [range.start.line, range.start.character, range.end.line, range.end.character], text: newText };
  };
  // The data of an action that the server lists without its edit: the finding it is for, as the
  // server numbered it for this document version, and which edit to compute.
  const lazyAction = (data, finding, uri, findings, title) => {
    if (typeof title !== 'string' || title === '' || title.length > 300 || !data || typeof data !== 'object' || findings.length !== 1 || data.uri !== uri ||
        data.version !== finding.version || data.generation !== finding.generation ||
        data.key !== finding.key || (data.action !== 'suppress' && data.action !== 'fix')) return undefined;
    const check = checkOfKey(data.key), code = findings[0].code;
    if (!check || check !== (code && typeof code === 'object' ? code.value : code)) return undefined;
    if (data.action === 'fix' && !fixCheck(check)) return undefined;
    return { action: data.action, key: data.key, title };
  };
  const checkOfKey = (key) => {
    const match = typeof key === 'string' ? /^[0-9]+:([a-z][a-z0-9-]{0,99})$/.exec(key) : null;
    return match ? match[1] : undefined;
  };
  // The one edit of a resolved action, as the server sent it: versioned for the analyzed document
  // version, in the analyzed document, and a single text edit.
  const resolvedEdit = (reply, guard) => {
    const changes = reply && reply.edit && reply.edit.documentChanges;
    if (!Array.isArray(changes) || changes.length !== 1) return undefined;
    const [change] = changes, id = change && change.textDocument;
    if (!id || id.uri !== guard.uri || id.version !== guard.version ||
        !Array.isArray(change.edits) || change.edits.length !== 1) return undefined;
    const [edit] = change.edits;
    return edit && typeof edit.newText === 'string' && validPositions(edit.range) ? edit : undefined;
  };
  const feature = {
    fillClientCapabilities(capabilities) {
      if (!enabled) return;
      capabilities.experimental = capabilities.experimental || {};
      capabilities.experimental.zp = {
        ...capabilities.experimental.zp,
        lintDiagnostics: true, responsiveAnalysis: true, showExplanation: true,
      };
    },
    initialize() {},
    getState() { return { kind: 'static' }; },
    clear() { latest.clear(); },
  };
  const middleware = {
    // Resolving happens in applyResolved, where the edit is checked; the editor's own resolve
    // of an action of this extension would only compute the edit twice.
    resolveCodeAction(item, token, next) {
      const lazy = item && item.command && item.command.command === 'zp.applyLintAction';
      return lazy ? item : next(item, token);
    },
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
      if (!enabled) {
        // Lint actions exist only behind the experimental setting; never let
        // one through unguarded, in particular a version-less edit.
        const passed = await next(document, range, context, token);
        return Array.isArray(passed) ? passed.filter((a) => !(a.diagnostics || []).some((d) => d.source === 'zp-lint')) : passed;
      }
      const version = document.version, uri = uriText(document.uri);
      const actions = await next(document, range, context, token);
      if (!active() || token.isCancellationRequested || document.isClosed ||
          documentFor(uri) !== document || document.version !== version) return [];
      return (actions || []).flatMap((action) => {
        const diagnostics = action.diagnostics || [];
        const findings = diagnostics.filter((d) => d.source === 'zp-lint');
        if (findings.length === 0) return action.command?.command === 'zp.showExplanation' ? [] : [action];
        const data = findings.map(findingData);
        if (data.some((d) => !d || d.uri !== uri || d.version !== version || !findingCurrent(d))) return [];
        const guard = { uri, version, generation: data[0].generation, session };
        if (data.some((d) => d.generation !== guard.generation)) return [];
        if (!action.command && !action.edit && action.data !== undefined) {
          // The server lists the action without its edit and computes it when asked. Run it
          // through a command, so that the edit is asked for, checked against the document
          // and applied in one guarded step (see applyResolved).
          const spec = lazyAction(action.data, data[0], uri, findings, action.title);
          if (!spec) return [];
          action.command = { title: action.title, command: 'zp.applyLintAction', arguments: [spec, guard] };
        } else if (action.command) {
          if (action.command.command !== 'zp.showExplanation' || action.edit ||
              !Array.isArray(action.command.arguments) || action.command.arguments.length !== 3) return [];
          action.command = { ...action.command, arguments: [...(action.command.arguments || []), guard] };
        } else if (action.edit) {
          // The LanguageClient converts the LSP edit to a WorkspaceEdit that
          // carries no document version. Surface it as a command instead; the
          // command applies a freshly built edit under a synchronous version
          // check (see applySuppression).
          const target = suppressionTarget(action.edit, uri, findings);
          const fix = target ? undefined : fixTarget(action.edit, uri, findings, document);
          if (!target && !fix) return [];
          action.edit = undefined;
          action.command = target ? {
            title: action.title, command: 'zp.applyLintSuppression',
            arguments: [target.line, target.text, guard],
          } : {
            title: action.title, command: 'zp.applyLintFix',
            arguments: [fix.check, fix.range, fix.text, guard],
          };
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
      await vscode.window.showInformationMessage(`ZeroParse ${check}: ${message}\n${explanation}`);
      return true;
    },
    // Applies a suppression only to the analyzed document version. VS Code's
    // extension host stamps each text edit of workspace.applyEdit with the
    // version of the document it holds at the moment of the call, and the
    // main thread refuses the edit when its model has a different version. The
    // check below and the call therefore run in one synchronous step: nothing
    // may be awaited between them, or an edit event could slip in and the
    // version stamp would describe a document that was never analyzed.
    applySuppression(line, text, guard) {
      if (!guardCurrent(guard) || !Number.isSafeInteger(line) || line < 0) return Promise.resolve(false);
      const document = documentFor(guard.uri);
      if (!document || document.version !== guard.version) return Promise.resolve(false);
      const check = typeof text === 'string' ? /^\/\/ lint:ignore ([a-z][a-z0-9-]{0,99})(\r\n|\n|\r)$/.exec(text) : null;
      if (!check || !suppressionText(check[1], text)) return Promise.resolve(false);
      if (Number.isInteger(document.lineCount) && line >= document.lineCount) return Promise.resolve(false);
      const edit = new vscode.WorkspaceEdit();
      edit.insert(document.uri, new vscode.Position(line, 0), text);
      return Promise.resolve(vscode.workspace.applyEdit(edit)).then((applied) => applied === true);
    },
    // Asks the server for the edit of a listed action, then applies it only if the document is
    // still at the analyzed version once the answer is in. The check and the call of
    // workspace.applyEdit (through applySuppression or applyFix) are one synchronous step.
    async applyResolved(spec, guard) {
      if (!active() || !guardCurrent(guard) || !spec || typeof resolveRequest !== 'function') return false;
      const check = checkOfKey(spec.key);
      if (!check || (spec.action !== 'suppress' && spec.action !== 'fix') || (spec.action === 'fix' && !fixCheck(check))) return false;
      if (typeof spec.title !== 'string' || spec.title === '' || spec.title.length > 300) return false;
      let reply;
      try {
        reply = await resolveRequest({
          title: spec.title, kind: 'quickfix',
          data: { uri: guard.uri, version: guard.version, generation: guard.generation, key: spec.key, action: spec.action },
        });
      } catch (error) {
        return false;
      }
      if (!guardCurrent(guard)) return false;
      const edit = resolvedEdit(reply, guard);
      if (!edit) return false;
      const { start, end } = edit.range;
      if (spec.action === 'suppress') {
        if (start.character !== 0 || end.line !== start.line || end.character !== 0 || !suppressionText(check, edit.newText)) return false;
        return this.applySuppression(start.line, edit.newText, guard);
      }
      return this.applyFix(check, [start.line, start.character, end.line, end.character], edit.newText, guard);
    },
    // Applies a fix under the same synchronous version check as a suppression
    // (see applySuppression), after judging the text it replaces once more.
    applyFix(check, range, text, guard) {
      if (!guardCurrent(guard) || !fixCheck(check) || typeof text !== 'string' || !Array.isArray(range) ||
          range.length !== 4) return Promise.resolve(false);
      const [startLine, startCharacter, endLine, endCharacter] = range;
      const r = { start: { line: startLine, character: startCharacter }, end: { line: endLine, character: endCharacter } };
      const document = documentFor(guard.uri);
      if (!validRange(r) || !document || document.version !== guard.version || typeof document.getText !== 'function' ||
          Number.isInteger(document.lineCount) && endLine > document.lineCount) return Promise.resolve(false);
      const old = document.getText(new vscode.Range(startLine, startCharacter, endLine, endCharacter));
      if (typeof old !== 'string' || !fixShapes[check](old, text)) return Promise.resolve(false);
      const edit = new vscode.WorkspaceEdit();
      edit.replace(document.uri, new vscode.Range(startLine, startCharacter, endLine, endCharacter), text);
      return Promise.resolve(vscode.workspace.applyEdit(edit)).then((applied) => applied === true);
    },
  };
}

module.exports = { createLintActions };
