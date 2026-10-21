# Check a File in a TypeScript Tool With the Same Grammar

**Problem.** The format of a settings file is defined by a grammar that a Go service already uses, and now a TypeScript
tool (a linter, a pre-commit hook, a web form) has to check the same files. You do not want a second parser that can
disagree with the first one, and you do not want to add a dependency to the tool.

Generate a TypeScript module from the grammar. It has no dependencies, returns the same trees and the same errors as the
engine, and with `-types` it returns typed values. The grammar recovers from errors, so one run reports every
syntax problem, and the tool adds its own checks on top of the values that were read.

```pego
// settings.pego
type Setting = { Key node[string]; Value node[string] }
type Item ~ Setting | error
type File = { Settings []Item }

rule main File = -ws ss:(s:setting -ws)* $$ => File{Settings: map($ss, (x) => x.$s)}

// A setting that does not parse is skipped up to the next ";" (but not past a line end).
rule setting Item = s:entry #recover(skip=[^;\n]+ ";"?) => $s

rule entry Setting = (
    k:name #atomic -ws "=" @error(message="expected '='") -ws v:num @error(message="expected a number")
    -ws ";" @error(message="expected ';'")
) => Setting{Key: $k, Value: $v}

rule name = [a-z_0-9]+ #atomic
rule num = [0-9]+ #atomic
rule ws = (&[ \t\r\n] .)*
```

Generate the module next to the tool. The `pego` command comes from `go install github.com/ornew/pego/cmd/pego@latest`:

```bash
pego gen -lang ts -types -g settings.pego -o settings.ts
```

The tool reads the text, reports the syntax errors, and checks the limits of the settings that were read:

```ts
// check.ts
import { parseAST, SyntaxErrors } from "./settings.ts";
import type { Setting } from "./settings.ts";

const limits: Record<string, number> = { port: 65535, workers: 64 };

const source = `port = 80;
workers = 4;
name = ;
port2 = 7;
workers = 640;
debug 1;
`;

// Line and column (1-based, in code points) of a position in code points.
function lineCol(text: string, pos: number): string {
  const before = Array.from(text).slice(0, pos);
  const line = before.filter((c) => c === "\n").length + 1;
  const col = before.length - before.lastIndexOf("\n");
  return `${line}:${col}`;
}

const { value, error } = parseAST(source);
let problems = 0;

if (error instanceof SyntaxErrors) {
  for (const e of error.errors) {
    console.log(`${e.line}:${e.col}: ${e.reason()}`);
    problems++;
  }
} else if (error !== null) {
  throw error; // not a syntax error: a bug or a limit
}

for (const s of value?.Settings ?? []) {
  // A setting that was recovered is an error value; its error is reported above.
  if (s === null || s.type !== "Setting") continue;
  const setting = s as Setting;
  if (setting.Key === null || setting.Value === null) continue;
  const limit = limits[setting.Key.value];
  if (limit !== undefined && Number(setting.Value.value) > limit) {
    console.log(`${lineCol(source, setting.Value.start)}: ${setting.Key.value} must be at most ${limit}`);
    problems++;
  }
}

const read = (value?.Settings ?? []).filter((s) => s !== null && s.type === "Setting").length;
console.log(`${problems} problems, ${read} settings read`);
```

Node.js 22.18 or later, Deno and Bun run TypeScript as it is:

```bash
node check.ts
```

## Expected Output

```text
3:8: expected a number
6:7: expected '='
5:11: workers must be at most 64
3 problems, 4 settings read
```

The two syntax errors come from the parser, and a Go program using the same grammar would report the same two (see
[Report several errors in one run](several-errors.md) for the Go side). The limit is the tool's own check, and it was
found in a setting that parsed. Four settings were read, and the two that could not be read are error values in the list.

## How It Works

- `parseAST` returns `{ value, error }` instead of throwing. When `#recover` skipped something, `error` is a
  `SyntaxErrors` and `value` is still the tree of what was read, so the tool can report both the syntax errors and its
  own findings in one run. Any other `error` is a failure with no tree.
- `-types` gives every grammar type an interface (`Setting`, `File`), so `setting.Key.value` is checked by the compiler.
  The items of `Settings` are a join, because a setting that was recovered is an error value, and a field that did not
  match is `null`.
- Positions count code points, like the Go engine, not UTF-16 units. `lineCol` counts code points for that reason; the
  `line` and `col` of a syntax error are already computed the same way.
- The module does not depend on Node.js: the same `settings.ts` runs in a browser, where the check can run on every
  keystroke of a form.

Type-check the tool as strictly as you like; the generated module is written to compile under `--strict`:

```bash
tsc --noEmit --strict --noUncheckedIndexedAccess --erasableSyntaxOnly --allowImportingTsExtensions --module nodenext check.ts settings.ts
```

## Variations and Limits

- Keep the module current by regenerating it in the build and failing when it differs; see
  [Ship a parser and keep it up to date](ship-a-parser.md), which does the same for Go.
- `parse` returns the untyped `Node` tree instead, with `field("Key")` lookups, and `marshal(node)` writes the JSON that
  the engine writes, byte for byte, which is useful for comparing the two sides in a test.
- The generated module has no streaming, no incremental documents and no backends. For those, the parser stays in Go; see
  the [guide to generated TypeScript parsers](../guide/typescript.md) for what is supported.
- Ints in actions are 64-bit; a value beyond 2^53 arrives as a `bigint`. Settings here are text, so this does not arise.
