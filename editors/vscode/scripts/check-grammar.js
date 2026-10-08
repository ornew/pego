// Checks the extension's static files: that the JSON files parse, that every regular expression of
// the TextMate grammar compiles with Oniguruma (the engine VS Code uses), and that the grammar
// tokenizes the example grammars of the repository and a sample with the expected scopes.
//
// Run with `npm run check` after `npm install`.
'use strict';

const fs = require('fs');
const path = require('path');
const oniguruma = require('vscode-oniguruma');
const vsctm = require('vscode-textmate');

const root = path.join(__dirname, '..');
const repo = path.join(root, '..', '..');
let failures = 0;

function fail(message) {
  console.error('FAIL: ' + message);
  failures++;
}

function readJSON(file) {
  try {
    return JSON.parse(fs.readFileSync(path.join(root, file), 'utf8'));
  } catch (err) {
    fail(`${file}: ${err.message}`);
    return undefined;
  }
}

// collectRegexes returns the regular expressions of a grammar rule and the rules it contains.
function collectRegexes(rule, where, out) {
  if (!rule || typeof rule !== 'object') {
    return out;
  }
  for (const key of ['match', 'begin', 'end', 'while']) {
    if (typeof rule[key] === 'string') {
      out.push({ where: `${where}.${key}`, source: rule[key] });
    }
  }
  for (const [key, value] of Object.entries(rule)) {
    if (Array.isArray(value)) {
      value.forEach((r, i) => collectRegexes(r, `${where}.${key}[${i}]`, out));
    } else if (typeof value === 'object') {
      collectRegexes(value, `${where}.${key}`, out);
    }
  }
  return out;
}

// expectations are substrings of the sample, a scope each must have (or, after "!", must not
// have), and optionally a context after which the substring is looked for.
const sample = [
  'package calc',
  '// A comment',
  'type Num terminal // trailing',
  'type Op struct {',
  '    Left  Num // the left operand',
  '    Right *Num',
  '}',
  'type Node = Op | Num | []Match',
  'def expr: Node = l:term rest:(op:"+" r:term)* -> foldl($l, $rest, (acc, i) => new Op{Left: $acc, Right: $i.r})',
  'def term = @(?0-9_\\-)+ / "\\u{1F600}\\q" #error(message="expected (a term)") #stream',
  'def calc = pratt {',
  '    skip " "*',
  '    operand num',
  '    level { infix left "+" -> $1 }',
  '}',
  'def main = ^^ expr? $$ [n = len($1) > 0 && true] -- _ _|_ . x{2,3} !y &z -w',
  'def string = "s" string',
  'def a = default_value typed_x',
  'def typed = typed new T{}',
  'type U = A',
  '    | B // the other',
  'def x² = y',
].join('\n');

const expectations = [
  ['package', 'keyword.other.package.pego'],
  ['calc', 'entity.name.namespace.pego'],
  ['// A comment', 'comment.line.double-slash.pego'],
  ['type', 'storage.type.pego'],
  ['Num', 'entity.name.type.pego'],
  ['terminal', 'storage.type.terminal.pego'],
  ['// trailing', 'comment.line.double-slash.pego'],
  ['struct', 'storage.type.struct.pego'],
  ['Left', 'variable.other.property.pego'],
  ['// the left operand', 'comment.line.double-slash.pego'],
  ['Right', 'variable.other.property.pego'],
  ['*', 'keyword.operator.type.pego'],
  ['Node', 'entity.name.type.pego'],
  ['|', 'keyword.operator.union.pego'],
  ['Match', 'support.type.builtin.pego'],
  ['def', 'storage.type.function.pego'],
  ['expr', 'entity.name.function.pego'],
  ['l', 'variable.other.capture.pego'],
  ['"+"', 'string.quoted.double.pego'],
  ['->', 'keyword.operator.arrow.pego'],
  ['foldl', 'support.function.builtin.pego'],
  ['$rest', 'variable.parameter.capture.pego'],
  ['=>', 'keyword.operator.arrow.pego'],
  ['new', 'keyword.operator.new.pego'],
  ['Op', 'entity.name.type.pego', 'new Op'],
  ['r', 'variable.other.property.pego', '$i.r'],
  ['term', 'entity.name.function.pego', 'def term'],
  ['@', 'keyword.operator.atomic.pego'],
  ['(?', 'punctuation.definition.character-class.begin.pego'],
  ['-', 'keyword.operator.range.pego', '0-9'],
  ['\\-', 'constant.character.escape.pego'],
  ['/', 'keyword.operator.choice.pego', ')+ /'],
  ['\\u{1F600}', 'constant.character.escape.pego'],
  ['\\q', 'invalid.illegal.escape.pego'],
  ['error', 'entity.other.attribute-name.pego'],
  ['message', 'variable.parameter.attribute.pego'],
  ['"expected (a term)"', 'string.quoted.double.pego'],
  ['stream', 'entity.other.attribute-name.pego'],
  ['pratt', 'keyword.control.pratt.pego'],
  ['skip', 'keyword.control.pratt.pego'],
  ['level', 'keyword.control.pratt.pego'],
  ['left', 'keyword.other.associativity.pego'],
  ['$1', 'variable.parameter.capture.pego'],
  ['^^', 'keyword.operator.anchor.pego'],
  ['?', 'keyword.operator.quantifier.pego', 'expr?'],
  ['$$', 'keyword.operator.anchor.pego'],
  ['len', 'support.function.builtin.pego'],
  ['&&', 'keyword.operator.pego'],
  ['true', 'constant.language.pego'],
  ['--', 'keyword.control.cut.pego'],
  ['_', 'constant.language.top.pego', ' _ '],
  ['_|_', 'constant.language.bottom.pego'],
  ['.', 'constant.language.any.pego', ' . '],
  ['{2,3}', 'keyword.operator.quantifier.pego'],
  ['!', 'keyword.operator.lookahead.pego', '!y'],
  ['-', 'keyword.operator.discard.pego', '-w'],
  ['string', 'entity.name.function.pego', 'def string'],
  ['string', '!support.type.builtin.pego', '"s" string'],
  // Keywords at the start of identifiers are not keywords.
  ['default_value', '!storage.type.function.pego'],
  ['typed_x', '!storage.type.pego'],
  ['typed', 'entity.name.function.pego', 'def typed'],
  ['typed', '!storage.type.pego', '= typed'],
  // The braces of new T{} are not a repetition.
  ['{}', '!keyword.operator.quantifier.pego'],
  // A union continued on the next line.
  ['|', 'keyword.operator.union.pego', '    | B'],
  ['B', 'entity.name.type.pego', '| B'],
  ['// the other', 'comment.line.double-slash.pego'],
  // Identifiers continue with decimal digits only, like the lexer's.
  ['²', '!entity.name.function.pego', 'def x²'],
];

async function main() {
  for (const file of ['package.json', 'language-configuration.json', '.vscode/launch.json']) {
    readJSON(file);
  }
  const grammarFile = 'syntaxes/pego.tmLanguage.json';
  const grammarJSON = readJSON(grammarFile);
  if (!grammarJSON) {
    return;
  }

  const wasm = fs.readFileSync(require.resolve('vscode-oniguruma/release/onig.wasm'));
  await oniguruma.loadWASM(wasm.buffer.slice(wasm.byteOffset, wasm.byteOffset + wasm.byteLength));
  const regexes = collectRegexes(grammarJSON, 'grammar', []);
  for (const { where, source } of regexes) {
    try {
      new oniguruma.OnigScanner([source]).dispose();
    } catch (err) {
      fail(`${where}: ${source}: ${err.message}`);
    }
  }
  console.log(`${regexes.length} regular expressions compile`);

  const registry = new vsctm.Registry({
    onigLib: Promise.resolve({
      createOnigScanner: (sources) => new oniguruma.OnigScanner(sources),
      createOnigString: (s) => new oniguruma.OnigString(s),
    }),
    loadGrammar: async (scopeName) =>
      scopeName === 'source.pego'
        ? vsctm.parseRawGrammar(fs.readFileSync(path.join(root, grammarFile), 'utf8'), grammarFile)
        : null,
  });
  const grammar = await registry.loadGrammar('source.pego');

  // tokenize returns the tokens of text with their offsets in it.
  function tokenize(text) {
    const tokens = [];
    let state = vsctm.INITIAL;
    let offset = 0;
    for (const line of text.split('\n')) {
      const r = grammar.tokenizeLine(line, state);
      for (const t of r.tokens) {
        tokens.push({ start: offset + t.startIndex, end: offset + t.endIndex, scopes: t.scopes });
      }
      state = r.ruleStack;
      offset += line.length + 1;
    }
    return tokens;
  }

  // The sample: each expectation's text, found after its context if one is given, must be covered
  // by tokens that all have the scope.
  const tokens = tokenize(sample);
  let from = 0;
  for (const [text, scope, context] of expectations) {
    let start;
    if (context) {
      const c = sample.indexOf(context, from);
      start = c < 0 ? -1 : sample.indexOf(text, c);
    } else {
      start = sample.indexOf(text, from);
    }
    if (start < 0) {
      fail(`sample: ${JSON.stringify(text)} not found after offset ${from}`);
      continue;
    }
    const end = start + text.length;
    const covering = tokens.filter((t) => t.start < end && t.end > start);
    const negated = scope.startsWith('!');
    const name = negated ? scope.slice(1) : scope;
    const bad = covering.filter((t) => t.scopes.includes(name) === negated);
    if (bad.length > 0) {
      const want = negated ? `not ${name}` : name;
      fail(`sample: ${JSON.stringify(text)} at ${start} has scopes ${JSON.stringify(bad[0].scopes)}, want ${want}`);
    }
    from = start;
  }
  console.log(`${expectations.length} scopes of the sample are as expected`);

  // The example grammars are valid, so nothing in them is marked invalid, and every line ends
  // outside strings, character classes and attribute arguments (their rules end on the line).
  const examples = path.join(repo, 'examples');
  let files = 0;
  for (const dir of fs.readdirSync(examples)) {
    const d = path.join(examples, dir);
    if (!fs.statSync(d).isDirectory()) {
      continue;
    }
    for (const name of fs.readdirSync(d).filter((n) => n.endsWith('.pego'))) {
      const file = path.join(d, name);
      const text = fs.readFileSync(file, 'utf8');
      let state = vsctm.INITIAL;
      text.split('\n').forEach((line, i) => {
        const r = grammar.tokenizeLine(line, state);
        for (const t of r.tokens) {
          const invalid = t.scopes.find((s) => s.startsWith('invalid.'));
          if (invalid) {
            fail(`${path.relative(repo, file)}:${i + 1}: ${invalid} at ${JSON.stringify(line.slice(t.startIndex, t.endIndex))}`);
          }
        }
        state = r.ruleStack;
      });
      files++;
    }
  }
  console.log(`${files} example grammars tokenized`);
}

main()
  .catch((err) => fail(err.stack || String(err)))
  .finally(() => {
    if (failures > 0) {
      console.error(`${failures} failure(s)`);
      process.exit(1);
    }
    console.log('ok');
  });
