// Portable invocation controls are merged into the generated runtime module.

/** A cooperative signal without a required DOM typing dependency. */
export interface CancellationSignal {
  readonly aborted: boolean;
  readonly reason?: unknown;
}

export type ParseInterruptionKind = "canceled" | "input-work-limit" | "step-work-limit";

/** An interrupted invocation returns no partial Node or typed result. */
export class ParseInterruptionError extends Error {
  readonly kind: ParseInterruptionKind;
  readonly limit: number;
  readonly used: number;
  readonly requested: number;

  constructor(kind: ParseInterruptionKind, limit = 0, used = 0, requested = 0, cause: unknown = undefined) {
    super(kind === "canceled" ? "parse canceled" : `${kind} exceeded: limit ${limit}, used ${used}, requested ${requested}`);
    this.name = "ParseInterruptionError";
    this.kind = kind;
    this.limit = limit;
    this.used = used;
    this.requested = requested;
    Object.defineProperty(this, "cause", { value: cause, configurable: true });
  }
}
// Interface merging exposes cause with ES2020 or later Error declarations.
export interface ParseInterruptionError { readonly cause: unknown }

interface ResolvedParseOptions {
  readonly unit: Unit;
  readonly maxDepth: number;
  signal: CancellationSignal | null;
  readonly maxInputWork: number;
  readonly maxSteps: number;
}

// Signal getters are caller code. Keep their thrown identity through the
// parser's Fatal and host-stack-overflow classification boundaries.
class SignalPanic {
  readonly value: unknown;
  constructor(value: unknown) { this.value = value; }
}

class WorkControl {
  private signal: CancellationSignal | null;
  private readonly maxInputWork: number;
  private readonly maxSteps: number;
  private inputUsed = 0;
  private stepsUsed = 0;
  private inputSinceCheck = 0;
  private stepsSinceCheck = 0;

  constructor(o: ResolvedParseOptions) {
    this.signal = o.signal;
    this.maxInputWork = o.maxInputWork;
    this.maxSteps = o.maxSteps;
  }

  release(): void { this.signal = null; }

  check(): void {
    const signal = this.signal;
    if (signal === null) return;
    let aborted: boolean;
    let reason: unknown;
    try {
      aborted = signal.aborted;
      if (aborted) reason = signal.reason;
    } catch (x) { throw new SignalPanic(x); }
    if (aborted) throw new Fatal(new ParseInterruptionError("canceled", 0, 0, 0, reason));
  }

  charge(input: number, steps: number): void {
    if (input >= 4096 - this.inputSinceCheck || steps >= 256 - this.stepsSinceCheck) {
      this.check();
      this.inputSinceCheck = this.stepsSinceCheck = 0;
    }
    if (this.maxInputWork !== 0 && input > this.maxInputWork - this.inputUsed) {
      this.check();
      throw new Fatal(new ParseInterruptionError("input-work-limit", this.maxInputWork, this.inputUsed, input));
    }
    if (this.maxSteps !== 0 && steps > this.maxSteps - this.stepsUsed) {
      this.check();
      throw new Fatal(new ParseInterruptionError("step-work-limit", this.maxSteps, this.stepsUsed, steps));
    }
    // Validate both reservations before committing either counter. Unlimited
    // counters need no cumulative number that could lose safe-integer precision.
    if (this.maxInputWork !== 0) this.inputUsed += input;
    if (this.maxSteps !== 0) this.stepsUsed += steps;
    this.inputSinceCheck = input >= 4096 ? 0 : this.inputSinceCheck + input;
    this.stepsSinceCheck = steps >= 256 ? 0 : this.stepsSinceCheck + steps;
  }
}

function withWorkControl<T>(o: ResolvedParseOptions, body: (control: WorkControl | null) => T, interrupted: (error: Error) => T): T {
  if (o.signal === null && o.maxInputWork === 0 && o.maxSteps === 0) return body(null);
  let control: WorkControl | null = null;
  try {
    control = new WorkControl(o);
    o.signal = null;
    control.check();
    const value = body(control);
    control.check();
    return value;
  } catch (x) {
    if (x instanceof SignalPanic) throw x.value;
    if (x instanceof Fatal) return interrupted(x.err);
    throw x;
  } finally {
    // Teardown must neither charge nor consult caller getters.
    control?.release();
    o.signal = null;
  }
}

function utf8EncodingLenWork(s: string, control: WorkControl): number {
  let n = s.length;
  for (let i = 0; i < s.length; i++) {
    const c = stringUnitWork(s, i, control);
    if (c < 0x80) {
      continue;
    }
    if (c < 0x800) {
      n++;
    } else if (c >= 0xd800 && c < 0xdc00 && i + 1 < s.length && (stringUnitWork(s, i + 1, control) & 0xfc00) === 0xdc00) {
      n += 2; // a pair: 2 units, 4 bytes
      i++;
    } else {
      n += 2; // a lone surrogate is encoded as U+FFFD
    }
  }
  return n;
}


function encodeUTF8Work(s: string, control: WorkControl): Uint8Array {
  const out = new Uint8Array(utf8EncodingLenWork(s, control));
  let j = 0;
  for (let i = 0; i < s.length; i++) {
    let c = stringUnitWork(s, i, control);
    if (c < 0x80) {
      control.charge(0, 1);
      out[j++] = c;
      continue;
    }
    if (c >= 0xd800 && c <= 0xdfff) {
      if (c < 0xdc00 && i + 1 < s.length && (stringUnitWork(s, i + 1, control) & 0xfc00) === 0xdc00) {
        c = 0x10000 + ((c - 0xd800) << 10) + (stringUnitWork(s, i + 1, control) - 0xdc00);
        i++;
      } else {
        c = 0xfffd;
      }
    }
    control.charge(0, c < 0x800 ? 2 : c < 0x10000 ? 3 : 4);
    if (c < 0x800) {
      out[j++] = 0xc0 | (c >> 6);
      out[j++] = 0x80 | (c & 0x3f);
    } else if (c < 0x10000) {
      out[j++] = 0xe0 | (c >> 12);
      out[j++] = 0x80 | ((c >> 6) & 0x3f);
      out[j++] = 0x80 | (c & 0x3f);
    } else {
      out[j++] = 0xf0 | (c >> 18);
      out[j++] = 0x80 | ((c >> 12) & 0x3f);
      out[j++] = 0x80 | ((c >> 6) & 0x3f);
      out[j++] = 0x80 | (c & 0x3f);
    }
  }
  return out;
}


function runeCountWork(s: string, control: WorkControl): number {
  let n = s.length;
  for (let i = 0; i + 1 < s.length; i++) {
    const c = stringUnitWork(s, i, control);
    if (c >= 0xd800 && c < 0xdc00 && (stringUnitWork(s, i + 1, control) & 0xfc00) === 0xdc00) {
      n--;
      i++;
    }
  }
  return n;
}


function wellFormedWork(s: string, control: WorkControl): string {
  const parts: string[] = [];
  let from = 0;
  for (let i = 0; i < s.length; i++) {
    const c = stringUnitWork(s, i, control);
    if (c >= 0xd800 && c <= 0xdfff) {
      if (c < 0xdc00 && i + 1 < s.length && (stringUnitWork(s, i + 1, control) & 0xfc00) === 0xdc00) {
        i++;
        continue;
      }
      parts.push(s.slice(from, i), replacementChar);
      from = i + 1;
    }
  }
  if (from === 0) {
    return s;
  }
  parts.push(s.slice(from));
  return joinStringsWork(parts, control);
}


function stringUnitWork(s: string, i: number, control: WorkControl): number {
  control.charge(1, 1);
  return s.charCodeAt(i);
}
function byteUnitWork(b: Uint8Array, i: number, n: number, control: WorkControl): number {
  control.charge(1, 1);
  return i < n ? b[i]! : 0;
}
function joinStringsWork(parts: readonly string[], control: WorkControl): string {
  for (const part of parts) {
    control.charge(0, 1);
    for (let i = 0; i < part.length; i += 4096) control.charge(Math.min(4096, part.length - i), 1);
  }
  const result = parts.join("");
  control.check();
  return result;
}
function codePointsStringWork(cps: Int32Array, control: WorkControl): string {
  const parts: string[] = [];
  for (let i = 0; i < cps.length; i += 4096) {
    const end = Math.min(i + 4096, cps.length);
    control.charge(end - i, 1);
    parts.push(String.fromCodePoint(...cps.subarray(i, end)));
  }
  return joinStringsWork(parts, control);
}

function decodeRuneWork(b: Uint8Array, i: number, n: number, control: WorkControl): { rune: number; size: number } {
  let size = 1;
  const c0 = byteUnitWork(b, i, n, control);
  if (c0 < 0x80) {
    return { rune: c0, size };
  }
  if (c0 < 0xc2 || c0 > 0xf4) {
    return { rune: 0xfffd, size };
  }
  const c1 = byteUnitWork(b, i + 1, n, control);
  if (c0 < 0xe0) {
    if ((c1 & 0xc0) !== 0x80) {
      return { rune: 0xfffd, size };
    }
    size = 2;
    return { rune: ((c0 & 0x1f) << 6) | (c1 & 0x3f), size };
  }
  const lo = c0 === 0xe0 ? 0xa0 : c0 === 0xf0 ? 0x90 : 0x80;
  const hi = c0 === 0xed ? 0x9f : c0 === 0xf4 ? 0x8f : 0xbf;
  if (c1 < lo || c1 > hi) {
    return { rune: 0xfffd, size };
  }
  const c2 = byteUnitWork(b, i + 2, n, control);
  if ((c2 & 0xc0) !== 0x80) {
    return { rune: 0xfffd, size };
  }
  if (c0 < 0xf0) {
    size = 3;
    return { rune: ((c0 & 0x0f) << 12) | ((c1 & 0x3f) << 6) | (c2 & 0x3f), size };
  }
  const c3 = byteUnitWork(b, i + 3, n, control);
  if ((c3 & 0xc0) !== 0x80) {
    return { rune: 0xfffd, size };
  }
  size = 4;
  return { rune: ((c0 & 0x07) << 18) | ((c1 & 0x3f) << 12) | ((c2 & 0x3f) << 6) | (c3 & 0x3f), size };
}


function equalStringsWork(a: string, b: string, control: WorkControl): boolean {
  if (a.length !== b.length) return false;
  for (let from = 0; from < a.length; from += 128) {
    const end = Math.min(from + 128, a.length);
    control.charge(0, end - from);
    if (a.slice(from, end) !== b.slice(from, end)) return false;
  }
  return true;
}

function copyNodesWork(items: readonly (Node | null)[], start: number, end: number, control: WorkControl): (Node | null)[] {
  const out: (Node | null)[] = new Array(end - start);
  for (let from = start; from < end; from += 128) {
    const stop = Math.min(from + 128, end);
    control.charge(0, stop - from);
    for (let i = from; i < stop; i++) out[i - start] = items[i]!;
  }
  return out;
}

// These scalar helpers preserve surrogateescape bytes, unlike input encoding.
function utf8LenWork(s: string, control: WorkControl): number {
  let n = s.length;
  for (let i = 0; i < s.length; i++) {
    const c = valueUnitWork(s, i, control);
    if (c < 0x80) continue;
    if (c < 0x800) {
      n++;
    } else if (c >= 0xd800 && c < 0xdc00 && i + 1 < s.length && (valueUnitWork(s, i + 1, control) & 0xfc00) === 0xdc00) {
      n += 2;
      i++;
    } else if (c < 0xdc80 || c > 0xdcff) {
      n += 2;
    }
  }
  return n;
}

function sliceStringWork(s: string, start: number, end: number, control: WorkControl): string {
  // Native slicing/joining and allocation cannot be preempted. Reserve the
  // visited extent in bounded chunks before publishing the native result.
  const from = Math.max(0, Math.min(s.length, start < 0 ? s.length + start : start));
  const to = Math.max(0, Math.min(s.length, end < 0 ? s.length + end : end));
  for (let i = from; i < to; i += 128) control.charge(0, Math.min(128, to - i));
  const result = s.slice(start, end);
  control.check();
  return result;
}

function isByteWork(s: string, i: number, control: WorkControl): boolean {
  const c = valueUnitWork(s, i, control);
  return c >= 0xdc80 && c <= 0xdcff && !(i > 0 && (valueUnitWork(s, i - 1, control) & 0xfc00) === 0xd800);
}

// Stream only the suffix bytes actually compared; keep byte escapes raw.
function* utf8ValueBytesWork(s: string, start: number, control: WorkControl) {
  for (let i = start; i < s.length; i++) {
    let c = valueUnitWork(s, i, control);
    if (c >= 0xd800 && c < 0xdc00 && i + 1 < s.length) {
      const low = valueUnitWork(s, i + 1, control);
      if ((low & 0xfc00) === 0xdc00) {
        c = 0x10000 + ((c - 0xd800) << 10) + (low - 0xdc00);
        i++;
      }
    }
    if (c >= 0xdc80 && c <= 0xdcff) {
      control.charge(0, 1);
      yield c & 0xff;
      continue;
    }
    if (c >= 0xd800 && c <= 0xdfff) c = 0xfffd;
    control.charge(0, c < 0x80 ? 1 : c < 0x800 ? 2 : c < 0x10000 ? 3 : 4);
    if (c < 0x80) {
      yield c;
    } else if (c < 0x800) {
      yield 0xc0 | (c >> 6); yield 0x80 | (c & 0x3f);
    } else if (c < 0x10000) {
      yield 0xe0 | (c >> 12); yield 0x80 | ((c >> 6) & 0x3f); yield 0x80 | (c & 0x3f);
    } else {
      yield 0xf0 | (c >> 18); yield 0x80 | ((c >> 12) & 0x3f); yield 0x80 | ((c >> 6) & 0x3f); yield 0x80 | (c & 0x3f);
    }
  }
}

function compareStringsWork(a: string, b: string, control: WorkControl): number {
  const n = Math.min(a.length, b.length);
  for (let i = 0; i < n; i++) {
    let x = valueUnitWork(a, i, control);
    let y = valueUnitWork(b, i, control);
    if (x !== y) {
      if (isByteWork(a, i, control) || isByteWork(b, i, control)) {
        const p = utf8ValueBytesWork(a, i, control);
        const q = utf8ValueBytesWork(b, i, control);
        for (;;) {
          control.charge(0, 2);
          const x = p.next();
          const y = q.next();
          if (x.done || y.done) return x.done === y.done ? 0 : x.done ? -1 : 1;
          if (x.value !== y.value) return x.value < y.value ? -1 : 1;
        }
      }
      if (x >= 0xd800 && y >= 0xd800) {
        x = x >= 0xe000 ? x - 0x800 : x + 0x2000;
        y = y >= 0xe000 ? y - 0x800 : y + 0x2000;
      }
      return x < y ? -1 : 1;
    }
  }
  return a.length < b.length ? -1 : a.length > b.length ? 1 : 0;
}

function valueUnitWork(s: string, i: number, control: WorkControl): number {
  control.charge(0, 1);
  return s.charCodeAt(i);
}

function runeCountValueWork(s: string, control: WorkControl): number {
  let n = s.length;
  for (let i = 0; i + 1 < s.length; i++) {
    const c = valueUnitWork(s, i, control);
    if (c >= 0xd800 && c < 0xdc00 && (valueUnitWork(s, i + 1, control) & 0xfc00) === 0xdc00) {
      n--;
      i++;
    }
  }
  return n;
}

function joinValueStringsWork(parts: readonly string[], control: WorkControl): string {
  for (const part of parts) {
    control.charge(0, 1);
    for (let i = 0; i < part.length; i += 128) control.charge(0, Math.min(128, part.length - i));
  }
  const result = parts.join("");
  control.check();
  return result;
}

// Public Node.field stays a caller operation; parsing/conversion share this
// internal visit counter and bounded string-name comparison.
function getFieldWork(n: Node, name: string, control: WorkControl | null): Value {
  if (control === null) return n.field(name);
  for (const field of n.fields) {
    control.charge(0, 1);
    if (equalStringsWork(field.name, name, control)) return field.value;
  }
  return null;
}

// Reserve bounded logical visits before native hashing, which cannot be polled.
function stringKeyWork(s: string, control: WorkControl): string {
  for (let i = 0; i < s.length; i += 128) control.charge(0, Math.min(128, s.length - i));
  return s;
}

function equalValuesWork(a: Value, b: Value, control: WorkControl): boolean {
  if (typeof a === "string" && typeof b === "string") return equalStringsWork(a, b, control);
  return a === b;
}

function copyErrorsWork(items: readonly SyntaxError[], start: number, end: number, control: WorkControl): SyntaxError[] {
  const out: SyntaxError[] = new Array(end - start);
  for (let from = start; from < end; from += 128) {
    const stop = Math.min(from + 128, end);
    control.charge(0, stop - from);
    for (let i = from; i < stop; i++) out[i - start] = items[i]!;
  }
  return out;
}

function copyIDsWork(items: readonly number[], start: number, end: number, control: WorkControl): number[] {
  const out: number[] = new Array(end - start);
  for (let from = start; from < end; from += 128) {
    const stop = Math.min(from + 128, end);
    control.charge(0, stop - from);
    for (let i = from; i < stop; i++) out[i - start] = items[i]!;
  }
  return out;
}

// Only newly collected, parser-owned expected strings may be sorted here.
function sortExpectedWork(values: string[], control: WorkControl): void {
  const n = values.length;
  if (n < 2) return;
  control.charge(0, n);
  let src = values, dst = new Array<string>(n);
  for (let width = 1; width < n;) {
    for (let lo = 0; lo < n;) {
      const mid = Math.min(lo + width, n), hi = Math.min(mid + width, n);
      let i = lo, j = mid, k = lo;
      while (i < mid && j < hi) {
        const order = compareStringsWork(src[i]!, src[j]!, control);
        control.charge(0, 1);
        dst[k++] = order <= 0 ? src[i++]! : src[j++]!;
      }
      while (i < mid) { control.charge(0, 1); dst[k++] = src[i++]!; }
      while (j < hi) { control.charge(0, 1); dst[k++] = src[j++]!; }
      lo = hi;
    }
    const old = src; src = dst; dst = old;
    if (width >= n - width) break;
    width *= 2;
  }
  if (src !== values) for (let i = 0; i < n; i++) { control.charge(0, 1); values[i] = src[i]!; }
}

function reserveDiagnosticTextWork(text: string, control: WorkControl): void {
  for (let i = 0; i < text.length; i += 128) control.charge(0, Math.min(128, text.length - i));
}

// Reserve engine-owned text before the native Error constructor joins it.
// Native joining/allocation remain non-preemptible; public constructors and
// post-return reason() calls keep their existing caller-owned behavior.
function reserveSyntaxMessageWork(line: number, col: number, expected: readonly string[], messages: readonly string[], control: WorkControl): void {
  reserveDiagnosticTextWork(`${line}:${col}: `, control);
  const parts = messages.length !== 0 ? messages : expected;
  if (messages.length === 0) reserveDiagnosticTextWork(expected.length !== 0 ? "syntax error: expected " : "syntax error", control);
  for (let i = 0; i < parts.length; i++) {
    control.charge(0, 1);
    if (i !== 0) control.charge(0, 2);
    reserveDiagnosticTextWork(parts[i]!, control);
  }
}

function reserveRecoveredMessageWork(errors: readonly SyntaxError[], control: WorkControl): void {
  for (let i = 0; i < errors.length; i++) {
    control.charge(0, 1);
    if (i !== 0) control.charge(0, 1);
    reserveDiagnosticTextWork(errors[i]!.message, control);
  }
}
