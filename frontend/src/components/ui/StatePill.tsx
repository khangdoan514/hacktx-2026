import type { State } from "@/lib/types";

const tone: Record<State, string> = { RUNNING: "accent", SUCCEEDED: "ok", FAILED: "err", CANCELLED: "err", PENDING: "" };

export default function StatePill({ state, big }: { state: State; big?: boolean }) {
  return (
    <span className={`tag ${tone[state]}`} style={big ? { height: 26, padding: "0 10px", borderRadius: 13, fontSize: 11.5 } : undefined}>
      <i className={`dot${state === "RUNNING" ? " live" : ""}`} />
      {state}
    </span>
  );
}

export const stateColor = (st: State) =>
  st === "RUNNING" ? "c-run" : st === "SUCCEEDED" ? "c-ok" : st === "PENDING" ? "c-idle" : "c-err";
