// Rule tracing. A parser generated without observability rejects the trace options.

/** An actual rule entry, completed call, or identifiable dispatch skip. */
export type TraceKind = "enter" | "exit" | "skipped";

/** Scalar event snapshot; positions use the invocation's selected unit. */
export interface TraceEvent {
  readonly kind: TraceKind;
  readonly rule: string;
  readonly level: number;
  readonly depth: number;
  readonly pos: number;
  readonly matched: boolean;
  readonly end: number;
  readonly memo: boolean;
  readonly evals: number;
  readonly examined: number;
  readonly examinedKnown: boolean;
  readonly skipReason: string;
}

export interface ObservationCall {
  readonly rule: string;
  readonly pos: number;
  readonly level: number;
  readonly depth: number;
  readonly selected: boolean;
  evals: number;
  memo: boolean;
}

// Remains wrapped until outside run and work-control error classification.
export class TraceFailure {
  readonly value: unknown;
  readonly scope: object;
  constructor(value: unknown, scope: object) { this.value = value; this.scope = scope; }
}

export class Observation {
  private callback: ((event: TraceEvent) => void) | null;
  private scope: object | null;
  private rules: Set<string> | null;
  private readonly maxDepth: number;
  protected readonly stack: ObservationCall[] = [];

  constructor(o: ResolvedParseOptions) {
    this.callback = o.trace;
    this.scope = o;
    this.rules = o.trace === null ? new Set() : o.traceRules.length === 0 ? null : new Set(o.traceRules);
    this.maxDepth = o.traceMaxDepth;
  }

  release(): void {
    this.callback = null;
    this.scope = null;
    this.rules = null;
    this.stack.length = 0;
  }

  private selected(rule: string): boolean {
    return this.rules === null || this.rules.has(rule) || (this.stack[this.stack.length - 1]?.selected ?? false);
  }

  private deliver(kind: TraceKind, call: ObservationCall, matched = false, end = 0, skipReason = ""): void {
    if (!call.selected || this.maxDepth > 0 && call.depth > this.maxDepth) return;
    // Delivered objects never double as internal stack frames. A callback may
    // retain or mutate its copy without changing later events or call counts.
    const event: TraceEvent = {
      kind, rule: call.rule, pos: call.pos, level: call.level, depth: call.depth,
      matched, end, memo: call.memo, evals: call.evals,
      examined: 0, examinedKnown: false, skipReason,
    };
    try { this.callback!(event); }
    catch (value) { throw new TraceFailure(value, this.scope!); }
  }

  enter(rule: string, pos: number, level: number): void {
    const call: ObservationCall = { rule, pos, level, depth: this.stack.length + 1,
      selected: this.selected(rule), evals: 0, memo: false };
    this.stack.push(call);
    this.deliver("enter", call);
  }

  evaluated(): void {
    const call = this.stack[this.stack.length - 1];
    if (call !== undefined) call.evals++;
  }

  memoHit(): void {
    const call = this.stack[this.stack.length - 1];
    if (call !== undefined) call.memo = true;
  }

  exit(matched: boolean, end: number): void {
    this.deliver("exit", this.stack.pop()!, matched, end);
  }

  skipped(rule: string, pos: number, level: number): void {
    this.deliver("skipped", { rule, pos, level, depth: this.stack.length + 1,
      selected: this.selected(rule), evals: 0, memo: false }, false, 0, "first-character");
  }
}

// This outer boundary must not expose callback values to withWorkControl's
// private Fatal/SignalPanic catches. It also clears option-owned references.
export function withObservedOptions<T>(o: ResolvedParseOptions, body: (control: WorkControl | null) => T, interrupted: (error: Error) => T): T {
  try {
    const value = withWorkControl(o, body, interrupted);
    return value;
  } catch (x) {
    if (x instanceof TraceFailure && x.scope === o) throw x.value;
    throw x;
  } finally {
    o.profile = null;
    o.profileClock = null;
    o.trace = null;
    o.traceRules = [];
  }
}
