// Checks the extension's static files: that the JSON files parse, that every regular expression of
// the TextMate grammar compiles with Oniguruma (the engine VS Code uses), and that the grammar
// tokenizes the example and ready-made parser grammars of the repository and a sample with the expected scopes.
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
  '/* outer',
  '  /* inner */ still comment',
  '*/',
  'type Num = { Text string } // trailing',
  'type Op = {',
  '    Left  Num // the left operand',
  '    Right *Num',
  '}',
  'type Node ~ Op | Num | []node[string] | error',
  'rule expr Node = l:term rest:(op:"+" r:term)* => foldl($l, $rest, (acc, i) => Op{Left: acc, Right: i.$r})',
  'rule term = [0-9_\\-]+ #atomic / "\\u{1F600}\\q" @error(message="expected (a term)")  #stream',
  'rule calc = #operators {',
  '    skip " "*',
  '    operand num',
  '    level { infix left "+" => $lhs }',
  '}',
  'rule main = ^^ expr? $$ {n := len($x) > 0 && true} #cut _ _|_ . x #repeat(2,3) !y &z -w',
  'rule string = "s" string',
  'rule old = "a" => $2',
  'rule a = default_value typed_x',
  'rule typed = typed T{}',
  'type U ~ (A',
  '    | B) // the other',
  'rule x² = y',
  'rule idx = xs:a+ => $xs[0][len($xs) - 1]',
  'rule pidx = xs:a+ {$xs[1] == nil} name [q-r]',
  'rule cacc = xs:(y:a)+ => map($xs, (i) => i.$y)',
  'rule cast = x:a => $x.(Num | []Num).startPos',
  'type Top ~ _ | []_|_ & Num',
  'type Tup = {',
  '  F node[Num, (Num,)]',
  '  G (Num, Pair)',
  '  H { Inner Num; Other []{ Deep Num } }',
  '  I Num',
  '}',
  'var depth int = 0',
  'var open = true',
  'rule scoped = {depth := depth + 1} "x"',
].join('\n');

const expectations = [
  ['package', 'keyword.other.package.zp'],
  ['calc', 'entity.name.namespace.zp'],
  ['// A comment', 'comment.line.double-slash.zp'],
  ['outer', 'comment.block.zp'],
  ['inner', 'comment.block.zp'],
  ['still comment', 'comment.block.zp'],
  ['type', 'storage.type.zp'],
  ['Num', 'entity.name.type.zp'],
  ['// trailing', 'comment.line.double-slash.zp'],
  ['=', 'keyword.operator.assignment.zp', 'type Op'],
  ['Left', 'variable.other.property.zp'],
  ['// the left operand', 'comment.line.double-slash.zp'],
  ['Right', 'variable.other.property.zp'],
  ['*', 'keyword.operator.type.zp'],
  ['Node', 'entity.name.type.zp'],
  ['~', 'keyword.operator.alias.zp'],
  ['|', 'keyword.operator.join.zp'],
  ['error', 'support.type.builtin.zp'],
  ['rule', 'storage.type.function.zp'],
  ['expr', 'entity.name.function.zp'],
  ['l', 'variable.other.capture.zp'],
  ['"+"', 'string.quoted.double.zp'],
  ['=>', 'keyword.operator.arrow.zp'],
  ['foldl', 'support.function.builtin.zp'],
  ['$rest', 'variable.parameter.capture.zp'],
  ['Op', 'entity.name.type.zp', '=> Op'],
  ['$r', 'variable.parameter.capture.zp', 'i.$r'],
  ['term', 'entity.name.function.zp', 'rule term'],
  ['[', 'punctuation.definition.character-class.begin.zp', '[0-9'],
  ['-', 'keyword.operator.range.zp', '0-9'],
  ['\\-', 'constant.character.escape.zp'],
  ['#', 'punctuation.definition.attribute.zp', '#atomic'],
  ['atomic', 'entity.other.attribute-name.zp'],
  ['/', 'keyword.operator.choice.zp'],
  ['\\u{1F600}', 'constant.character.escape.zp'],
  ['\\q', 'invalid.illegal.escape.zp'],
  ['error', 'entity.other.attribute-name.zp'],
  ['message', 'variable.parameter.attribute.zp'],
  ['"expected (a term)"', 'string.quoted.double.zp'],
  ['stream', 'entity.other.attribute-name.zp'],
  ['#operators', 'keyword.control.operators-rule.zp'],
  ['skip', 'keyword.control.operators-rule.zp'],
  ['level', 'keyword.control.operators-rule.zp'],
  ['left', 'keyword.other.associativity.zp'],
  ['$lhs', 'variable.parameter.capture.zp'],
  ['^^', 'keyword.operator.anchor.zp'],
  ['?', 'keyword.operator.quantifier.zp', 'expr?'],
  ['$$', 'keyword.operator.anchor.zp'],
  ['n', '!variable.other.capture.zp', '{n'],
  [':=', 'keyword.operator.assignment.zp'],
  ['len', 'support.function.builtin.zp'],
  ['&&', 'keyword.operator.zp'],
  ['true', 'constant.language.zp'],
  ['#cut', 'keyword.control.cut.zp'],
  ['_', 'constant.language.top.zp', ' _ '],
  ['_|_', 'constant.language.bottom.zp'],
  ['.', 'constant.language.any.zp', ' . '],
  ['repeat', 'entity.other.attribute-name.zp', '#repeat'],
  ['!', 'keyword.operator.lookahead.zp', '!y'],
  ['-', 'keyword.operator.discard.zp', '-w'],
  ['string', 'entity.name.function.zp', 'rule string'],
  ['string', '!support.type.builtin.zp', '"s" string'],
  ['$2', 'invalid.deprecated.positional-capture.zp'],
  ['default_value', '!storage.type.function.zp'],
  ['typed_x', '!storage.type.zp'],
  ['typed', 'entity.name.function.zp', 'rule typed'],
  ['typed', '!storage.type.zp', '= typed'],
  ['T', 'entity.name.type.zp', 'typed T'],
  ['{}', '!keyword.operator.quantifier.zp'],
  ['|', 'keyword.operator.join.zp', '    | B'],
  ['B', 'entity.name.type.zp', '| B'],
  ['// the other', 'comment.line.double-slash.zp'],
  ['²', '!entity.name.function.zp', 'rule x²'],
  ['[', '!punctuation.definition.character-class.begin.zp', '[0]'],
  ['len', 'support.function.builtin.zp', 'len($xs) - 1'],
  ['[', '!punctuation.definition.character-class.begin.zp', '[1]'],
  ['[', 'punctuation.definition.character-class.begin.zp', '[q-r]'],
  ['$y', 'variable.parameter.capture.zp', 'i.$y'],
  ['Num', 'entity.name.type.zp', '(Num |'],
  ['Num', 'entity.name.type.zp', '[]Num)'],
  ['startPos', 'variable.other.property.zp', '.startPos'],
  ['_', 'support.type.builtin.zp', '~ _ '],
  ['_|_', 'support.type.builtin.zp', '[]_|_'],
  ['&', 'keyword.operator.meet.zp', '_ & Num'],
  ['node', 'support.type.builtin.zp', 'F node['],
  ['[', 'punctuation.bracket.zp', 'node[Num'],
  ['Num', 'entity.name.type.zp', ', (Num,)'],
  ['G', 'variable.other.property.zp', '  G ('],
  ['Pair', 'entity.name.type.zp', 'Num, Pair'],
  ['Inner', 'variable.other.property.zp', '{ Inner Num'],
  ['Num', 'entity.name.type.zp', 'Inner Num;'],
  ['Other', 'variable.other.property.zp', '; Other'],
  ['Deep', 'variable.other.property.zp', '{ Deep'],
  ['I', 'variable.other.property.zp', '  I Num'],
  ['var', 'storage.type.zp', 'var depth'],
  ['depth', 'variable.other.zp', 'var depth'],
  ['int', 'support.type.builtin.zp', 'depth int'],
  ['=', 'keyword.operator.assignment.zp', 'int ='],
  ['var', 'storage.type.zp', 'var open'],
  ['open', 'variable.other.zp', 'var open'],
  ['depth', '!variable.other.zp', 'scoped = {depth'],
];

async function main() {
  for (const file of ['package.json', 'language-configuration.json', '.vscode/launch.json']) {
    readJSON(file);
  }
  const grammarFile = 'syntaxes/zp.tmLanguage.json';
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
      scopeName === 'source.zp'
        ? vsctm.parseRawGrammar(fs.readFileSync(path.join(root, grammarFile), 'utf8'), grammarFile)
        : null,
  });
  const grammar = await registry.loadGrammar('source.zp');

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

  // The example and ready-made parser grammars are valid, so nothing in them is marked invalid.
  let files = 0;
  for (const top of ['examples', 'parsers']) {
    const base = path.join(repo, top);
    for (const dir of fs.readdirSync(base)) {
      const d = path.join(base, dir);
      if (!fs.statSync(d).isDirectory()) {
        continue;
      }
      for (const name of fs.readdirSync(d).filter((n) => n.endsWith('.zp'))) {
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
  }
  console.log(`${files} example and parser grammars tokenized`);
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
