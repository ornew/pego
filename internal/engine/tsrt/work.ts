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

