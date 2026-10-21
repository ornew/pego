// Rule profiling; execution history belongs to each invocation. A parser generated without
// observability rejects the profile options.

export interface ProfileLocation { pos: number; level: number }
export interface SkipReasonCount { reason: string; count: number }

/** Owned counters; time and selfTime are milliseconds from a monotonic clock. */
export interface RuleProfile {
  rule: string;
  entered: number;
  calls: number;
  evals: number;
  memoHits: number;
  matched: number;
  failed: number;
  consumed: number;
  skipped: number;
  skipReasons: SkipReasonCount[];
  repeats: number;
  maxEvals: number;
  maxEvalsAt: ProfileLocation;
  examined: number;
  examinedKnown: boolean;
  wasted: number;
  wastedKnown: boolean;
  time: number;
  selfTime: number;
}

/** Owned, rule-sorted snapshot. Completed counters exclude unfinished calls. */
export interface ProfileSnapshot {
  parses: number;
  entered: number;
  calls: number;
  evals: number;
  memoHits: number;
  skipped: number;
  time: number;
  rules: RuleProfile[];
}

export interface ProfileData { totals: ProfileSnapshot; rows: Map<string, RuleProfile> }
// Weak keys let a discarded sink be collected; values contain only scalar rows.
export const profileData = new WeakMap<Profile, ProfileData>();

/** Accumulates across parses without retaining input, nodes or call history. */
export class Profile {
  constructor() {
    profileData.set(this, { totals: { parses: 0, entered: 0, calls: 0, evals: 0, memoHits: 0, skipped: 0, time: 0, rules: [] }, rows: new Map() });
  }

  /** Returns independent rows and nested arrays, sorted for deterministic JSON. */
  snapshot(): ProfileSnapshot {
    const data = profileData.get(this)!;
    const rules = Array.from(data.rows.values(), row => ({ ...row,
      maxEvalsAt: { ...row.maxEvalsAt },
      skipReasons: row.skipReasons.map(reason => ({ ...reason })).sort((a, b) => a.reason < b.reason ? -1 : a.reason > b.reason ? 1 : 0),
    })).sort((a, b) => a.rule < b.rule ? -1 : a.rule > b.rule ? 1 : 0);
    return { ...data.totals, rules };
  }
}

// Avoid a DOM or Node type dependency, and never read the clock when disabled.
export function resolveProfileClock(): (() => number) | Error {
  const host = (globalThis as { performance?: { now?: unknown } }).performance;
  const now = host?.now;
  if (typeof now !== "function") return new Error("profiling requires host performance.now()");
  return now.bind(host) as () => number;
}

export interface ProfileFrame { row: RuleProfile; start: number; child: number }

export class ProfileObservation extends Observation {
  private data: ProfileData | null;
  private clock: (() => number) | null;
  private profileScope: object | null;
  private lastTime: number | null = null;
  private readonly frames: ProfileFrame[] = [];
  private readonly seen = new Map<string, Map<number, Map<number, number>>>();
  private readonly open = new Map<string, number>();

  constructor(o: ResolvedParseOptions) {
    super(o);
    this.data = profileData.get(o.profile!)!;
    this.clock = o.profileClock;
    this.profileScope = o;
  }

  override release(): void {
    this.data = null;
    this.clock = null;
    this.profileScope = null;
    this.lastTime = null;
    this.frames.length = 0;
    this.seen.clear();
    this.open.clear();
    super.release();
  }

  private now(): number {
    try {
      const value = this.clock!();
      if (!Number.isFinite(value)) throw new Error("profiling clock must return a finite number");
      if (this.lastTime !== null && value < this.lastTime) throw new Error("profiling clock must be monotonic");
      this.lastTime = value;
      return value;
    } catch (value) { throw new TraceFailure(value, this.profileScope!); }
  }

  private row(rule: string): RuleProfile {
    let row = this.data!.rows.get(rule);
    if (row === undefined) {
      row = { rule, entered: 0, calls: 0, evals: 0, memoHits: 0, matched: 0, failed: 0, consumed: 0, skipped: 0, skipReasons: [], repeats: 0,
        maxEvals: 0, maxEvalsAt: { pos: 0, level: 0 }, examined: 0, examinedKnown: false, wasted: 0, wastedKnown: false, time: 0, selfTime: 0 };
      this.data!.rows.set(rule, row);
    }
    return row;
  }

  override enter(rule: string, pos: number, level: number): void {
    const start = this.now();
    const totals = this.data!.totals, row = this.row(rule);
    if (this.frames.length === 0) totals.parses++;
    totals.entered++;
    row.entered++;
    this.open.set(rule, (this.open.get(rule) ?? 0) + 1);
    this.frames.push({ row, start, child: 0 });
    super.enter(rule, pos, level);
  }

  override exit(matched: boolean, end: number): void {
    const call = this.stack[this.stack.length - 1]!;
    let callbackFailed = false;
    try { super.exit(matched, end); }
    catch (value) { callbackFailed = true; throw value; }
    finally {
      // A secondary host clock failure must not replace a callback's throw.
      try { this.completed(call, matched, end); }
      catch (value) { if (!callbackFailed) throw value; }
    }
  }

  private completed(call: ObservationCall, matched: boolean, end: number): void {
    const now = this.now(), frame = this.frames.pop()!;
    const elapsed = now - frame.start;
    const row = frame.row, totals = this.data!.totals;
    row.selfTime += Math.max(0, elapsed - frame.child);
    const open = this.open.get(call.rule)! - 1;
    this.open.set(call.rule, open);
    if (open === 0) row.time += elapsed;
    const parent = this.frames[this.frames.length - 1];
    if (parent === undefined) totals.time += elapsed;
    else parent.child += elapsed;
    row.calls++; totals.calls++;
    row.evals += call.evals; totals.evals += call.evals;
    if (call.memo) { row.memoHits++; totals.memoHits++; }
    if (matched) { row.matched++; row.consumed += end - call.pos; }
    else row.failed++;
    if (!call.memo && call.evals > 0) {
      let positions = this.seen.get(call.rule);
      if (positions === undefined) { positions = new Map(); this.seen.set(call.rule, positions); }
      let levels = positions.get(call.pos);
      if (levels === undefined) { levels = new Map(); positions.set(call.pos, levels); }
      const n = (levels.get(call.level) ?? 0) + 1;
      levels.set(call.level, n);
      if (n > 1) row.repeats++;
      if (n > row.maxEvals) { row.maxEvals = n; row.maxEvalsAt = { pos: call.pos, level: call.level }; }
    }
  }

  override skipped(rule: string, pos: number, level: number): void {
    const row = this.row(rule);
    this.data!.totals.skipped++;
    row.skipped++;
    let reason = row.skipReasons.find(x => x.reason === "first-character");
    if (reason === undefined) { reason = { reason: "first-character", count: 0 }; row.skipReasons.push(reason); }
    reason.count++;
    super.skipped(rule, pos, level);
  }
}
