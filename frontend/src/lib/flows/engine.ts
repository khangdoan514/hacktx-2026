import { SCENARIOS, type Scenario } from "./scenarios";

// Flow simulation state machine from FRONTEND.md §8. Pure TypeScript with no
// React, so the hook, a test or a future SSE source can all drive it.

export const STAGES = [
  { key: "brief", label: "Brief", sub: "Intake", ms: 1500, gate: false },
  { key: "scout", label: "Scout", sub: "SerpAPI", ms: 3600, gate: false },
  { key: "map", label: "Map", sub: "Landscape", ms: 2600, gate: false },
  { key: "ideate", label: "Ideate", sub: "NMC² GPUs", ms: 4600, gate: false },
  { key: "novelty", label: "Novelty", sub: "Gate", ms: 2600, gate: true },
  { key: "panel", label: "Consult", sub: "3 critics", ms: 5200, gate: false },
  { key: "decide", label: "Decide", sub: "Gate", ms: 2600, gate: true },
  { key: "deliver", label: "Deliver", sub: "Brief", ms: 1600, gate: false },
] as const;

export type StageState = "idle" | "run" | "done" | "wait";
export type IdeaStatus = "Drafted" | "Passed novelty" | "In debate" | "Shortlisted";

export const LANE_SPEED = [1, 1.12, 0.92];
export const TICK_MS = 120;
const LOOP_MS = 4200;
const AUTO_APPROVE_MS = 15000;
const RECYCLE_MS = 6500;
const FIRST_CODE = 1041;
const INITIAL_STAGES = [4, 5, 1];

export interface Lane {
  i: number;
  /** 1–3, maps to --f1..--f3 */
  color: number;
  code: string;
  s: Scenario;
  states: StageState[];
  mains: string[];
  subs: string[];
  stage: number;
  t: number;
  dur: number;
  p: number;
  pivoted: boolean;
  escalated: boolean;
  waiting: boolean;
  done: boolean;
  ideaStatus: IdeaStatus | null;
  loopUntil: number;
  autoApproveAt: number;
  recycleAt: number;
}

export interface Decision {
  id: number;
  at: number;
  code: string;
  color: number;
  agent: string;
  what: string;
  outcome: string;
  /** Absent for decisions that come from real run events. */
  conf?: number;
  why?: string;
  needsYou?: boolean;
  resolved?: boolean;
}

export interface FlowSnapshot {
  lanes: Lane[];
  decisions: Decision[];
  waiting: number;
  delivered: number;
}

type Toast = (message: string) => void;

export class FlowSim {
  lanes: Lane[] = [];
  decisions: Decision[] = [];
  delivered = 0;
  private nextCode = FIRST_CODE + 3;
  private nextScenario = 3;
  private seq = 0;

  constructor(now: number, private toast: Toast = () => {}) {
    INITIAL_STAGES.forEach((stage, i) => this.lanes.push(this.seedLane(i, now, stage)));
  }

  get waiting() {
    return this.lanes.filter((l) => l.waiting).length;
  }

  snapshot(): FlowSnapshot {
    return {
      lanes: this.lanes.map((l) => ({ ...l, states: [...l.states], mains: [...l.mains], subs: [...l.subs] })),
      decisions: [...this.decisions].sort((a, b) => b.at - a.at || b.id - a.id),
      waiting: this.waiting,
      delivered: this.delivered,
    };
  }

  tick(now: number) {
    for (const lane of this.lanes) this.tickLane(lane, now);
  }

  /** Approve the ranking on a waiting flow. `by` is "You" or the auto-approver. */
  approve(code?: string, by = "You") {
    const lane = this.findWaiting(code);
    if (!lane) return;
    const now = Date.now();
    this.log(lane, now, by, "Approved the ranking", "Decide", 0.9);
    this.markResolved(lane);
    this.completeDecide(lane, now);
  }

  anotherRound(code?: string) {
    const lane = this.findWaiting(code);
    if (!lane) return;
    const now = Date.now();
    this.log(lane, now, "You", "Asked the panel for another round", "Challenge", 0.79);
    this.markResolved(lane);
    lane.waiting = false;
    lane.escalated = true;
    lane.states[6] = "idle";
    this.setStage(lane, 5, now);
  }

  private findWaiting(code?: string) {
    return this.lanes.find((l) => l.waiting && (!code || l.code === code));
  }

  private markResolved(lane: Lane) {
    for (const d of this.decisions) if (d.code === lane.code && d.needsYou) d.resolved = true;
  }

  private newLane(i: number, scenarioIndex: number, code: number): Lane {
    const s = SCENARIOS[scenarioIndex % SCENARIOS.length];
    return {
      i, color: i + 1, code: `F-${code}`, s,
      states: STAGES.map(() => "idle" as StageState),
      mains: STAGES.map(() => ""), subs: STAGES.map(() => ""),
      stage: 0, t: 0, dur: 1, p: 0,
      pivoted: false, escalated: false, waiting: false, done: false,
      ideaStatus: null, loopUntil: 0, autoApproveAt: 0, recycleAt: 0,
    };
  }

  private seedLane(i: number, now: number, stage: number): Lane {
    const lane = this.newLane(i, i, FIRST_CODE + i);
    // Earlier stages complete silently, with decisions logged in the past.
    for (let k = 0; k < stage; k++) {
      this.completeStage(lane, k, now - (stage - k) * 20_000);
      lane.states[k] = "done";
    }
    this.setStage(lane, stage, now - Math.round(STAGES[stage].ms * LANE_SPEED[i] * 0.45));
    return lane;
  }

  private setStage(lane: Lane, stage: number, now: number) {
    lane.stage = stage;
    lane.states[stage] = "run";
    lane.t = now;
    lane.dur = STAGES[stage].ms * LANE_SPEED[lane.i];
    lane.p = 0;
    const s = lane.s;
    const [main, sub] = ((): [string, string] => {
      switch (STAGES[stage].key) {
        case "brief": return ["reading", s.client];
        case "scout": return ["searching", s.engines.join(" · ")];
        case "map": return ["clustering", `${s.results} sources`];
        case "ideate": return ["generating", "16 GPUs"];
        case "novelty": return ["scoring", "bar 0.70"];
        case "panel": return ["round 1/2", "3 critics"];
        case "decide": return ["ranking", "nov × feas"];
        default: return ["writing", "consultant brief"];
      }
    })();
    lane.mains[stage] = main;
    lane.subs[stage] = sub;
  }

  private tickLane(lane: Lane, now: number) {
    if (lane.done) {
      if (now >= lane.recycleAt) this.recycle(lane, now);
      return;
    }
    if (lane.waiting) {
      if (now >= lane.autoApproveAt) this.approve(lane.code, "Engagement lead");
      return;
    }
    if (lane.loopUntil) {
      if (now < lane.loopUntil) return;
      lane.loopUntil = 0;
      this.setStage(lane, 3, now);
      lane.subs[3] = "round 2";
      return;
    }
    lane.p = Math.min(1, (now - lane.t) / lane.dur);
    const s = lane.s;
    switch (STAGES[lane.stage].key) {
      case "scout":
        lane.mains[1] = "searching";
        lane.subs[1] = `${Math.round(s.results * lane.p)} via SerpAPI`;
        break;
      case "ideate":
        lane.mains[3] = "generating";
        lane.subs[3] = `${Math.round(s.ideas * lane.p)} ideas`;
        break;
      case "panel":
        lane.mains[5] = lane.p < 0.5 ? "round 1/2" : "round 2/2";
        break;
    }
    if (lane.p >= 1) this.finishStage(lane, now);
  }

  private finishStage(lane: Lane, now: number) {
    const stage = lane.stage;
    const key = STAGES[stage].key;
    const s = lane.s;
    if (key === "novelty" && s.pivot && !lane.pivoted) {
      lane.pivoted = true;
      this.log(lane, now, "Novelty Judge", "Sent the ideas back for another round", "Send back", 0.77,
        `Only 2 of ${s.ideas} ideas cleared the 0.70 novelty bar. Hardware is crowded, so the Ideator will explore service models instead.`);
      lane.states[3] = "idle";
      lane.states[4] = "idle";
      lane.mains[3] = lane.subs[3] = lane.mains[4] = lane.subs[4] = "";
      lane.stage = 3;
      lane.loopUntil = now + LOOP_MS;
      return;
    }
    if (key === "decide" && s.escalate && !lane.escalated) {
      lane.escalated = true;
      lane.waiting = true;
      lane.states[6] = "wait";
      lane.mains[6] = "your call";
      lane.subs[6] = "conf 0.64";
      lane.autoApproveAt = now + AUTO_APPROVE_MS;
      this.log(lane, now, "Lead Consultant", "Asked you to break a tie", "Needs you", 0.64, s.why.decide, true);
      return;
    }
    this.completeStage(lane, stage, now);
    lane.states[stage] = "done";
    if (key === "deliver") {
      lane.done = true;
      lane.recycleAt = now + RECYCLE_MS;
      this.delivered += 1;
      this.toast(`${lane.code} delivered to ${s.client}`);
      return;
    }
    this.setStage(lane, stage + 1, now);
  }

  /** Sets the done text and logs the stage's decision. */
  private completeStage(lane: Lane, stage: number, at: number) {
    const s = lane.s;
    const key = STAGES[stage].key;
    const set = (main: string, sub: string) => { lane.mains[stage] = main; lane.subs[stage] = sub; };
    const pruned = s.ideas - s.kept;
    switch (key) {
      case "brief": set("parsed", "4 constraints"); break;
      case "scout":
        set(`${s.results} results`, `SerpAPI · ${s.engines.join(" · ")}`);
        this.log(lane, at, "Scout", `Searched ${s.engines.join(" and ")} via SerpAPI`, "Route", 0.88, s.why.scout);
        break;
      case "map":
        set(`${s.clusters} clusters`, "2 merged");
        this.log(lane, at, "Cartographer", "Merged 2 overlapping clusters", "Merge", 0.81, s.why.map);
        break;
      case "ideate":
        set(`${s.ideas} ideas`, lane.pivoted ? "round 2" : "round 1");
        lane.ideaStatus = "Drafted";
        break;
      case "novelty":
        set(`${s.kept} kept`, `${pruned} pruned`);
        this.log(lane, at, "Novelty Judge", `Kept ${s.kept} of ${s.ideas} ideas`, "Keep", 0.86,
          `The other ${pruned} matched existing products with more than 0.70 similarity in SerpAPI search results.`);
        lane.ideaStatus = "Passed novelty";
        break;
      case "panel":
        set("2 rounds", "1 challenged");
        this.log(lane, at, "Consultant panel", "Challenged one idea and kept the rest", "Challenge", 0.79, s.why.panel);
        lane.ideaStatus = "In debate";
        break;
      case "decide":
        this.completeDecide(lane, at, true);
        break;
      case "deliver":
        set("brief ready", "12 pages");
        this.log(lane, at, "Lead Consultant", `Delivered the brief to ${s.client}`, "Deliver", 0.92);
        break;
    }
  }

  /** Choose the top 3. From the gate this also starts Deliver; in-stage the caller advances. */
  private completeDecide(lane: Lane, at: number, inStage = false) {
    const best = Math.max(...lane.s.list.map(([, n, f]) => n * f)).toFixed(2);
    lane.waiting = false;
    lane.states[6] = "done";
    lane.mains[6] = "top 3 chosen";
    lane.subs[6] = `best ${best}`;
    lane.ideaStatus = "Shortlisted";
    if (inStage) this.log(lane, at, "Lead Consultant", "Chose the top 3 ideas", "Decide", 0.9, lane.s.why.decide);
    else this.setStage(lane, 7, at);
  }

  private recycle(lane: Lane, now: number) {
    const fresh = this.newLane(lane.i, this.nextScenario, this.nextCode);
    this.nextScenario = (this.nextScenario + 1) % SCENARIOS.length;
    this.nextCode += 1;
    Object.assign(lane, fresh);
    this.setStage(lane, 0, now);
  }

  private log(lane: Lane, at: number, agent: string, what: string, outcome: string, conf: number, why?: string, needsYou?: boolean) {
    this.decisions.push({ id: ++this.seq, at, code: lane.code, color: lane.color, agent, what, outcome, conf, why, needsYou });
    if (this.decisions.length > 60) this.decisions.splice(0, this.decisions.length - 60);
  }
}
