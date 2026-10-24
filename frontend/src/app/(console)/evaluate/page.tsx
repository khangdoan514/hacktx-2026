"use client";

import { useRef, useState } from "react";
import { agentById } from "@/config/agents";
import { evalSuites, type EvalSuite } from "@/config/workspace";
import PageHeader from "@/components/shell/PageHeader";
import Icon from "@/components/ui/Icon";
import { useStore } from "@/lib/store";
import s from "./evaluate.module.css";

type Result = "pass" | "fail" | "running" | "queued";

const initial = (suite: EvalSuite): Record<string, Result> => {
  const passed = suite.history.at(-1)?.passed ?? 0;
  return Object.fromEntries(suite.cases.map((c, i) => [c.id, i < passed ? "pass" : "fail"]));
};

export default function EvaluatePage() {
  const total = evalSuites.reduce((a, x) => a + x.cases.length, 0);
  const passed = evalSuites.reduce((a, x) => a + (x.history.at(-1)?.passed ?? 0), 0);
  return (
    <>
      <PageHeader
        title="Evaluate"
        sub="Test suites that must pass before a version ships."
        code={{
          cli: "nmc3 eval run ideas-smoke --agent idea-consultant@1.0.0\nnmc3 eval compare idea-consultant 0.9.0 1.0.0\nnmc3 eval run --all --fail-under 0.9   # in CI",
          sdk: `const result = await nmc3.evals.run({\n  suite: "ideas-smoke",\n  agent: "idea-consultant@1.0.0",\n});\nif (result.passRate < 0.9) process.exit(1);`,
        }}
      />
      <div className={`${s.summary} rise`}>
        <div><small>suites</small><b>{evalSuites.length}</b></div>
        <div><small>cases</small><b>{total}</b></div>
        <div><small>passing</small><b>{Math.round((passed / total) * 100)}%</b></div>
        <div><small>gate</small><b>≥ 90%</b></div>
      </div>
      <div className={s.grid}>
        {evalSuites.map((suite, i) => <Suite key={suite.id} suite={suite} d={i * 50} />)}
      </div>
    </>
  );
}

function Suite({ suite, d }: { suite: EvalSuite; d: number }) {
  const { toast, openAgent } = useStore();
  const [res, setRes] = useState(() => initial(suite));
  const [busy, setBusy] = useState(false);
  const timers = useRef<ReturnType<typeof setTimeout>[]>([]);
  const agent = agentById(suite.agent)!;
  const pass = Object.values(res).filter((r) => r === "pass").length;

  const run = () => {
    timers.current.forEach(clearTimeout);
    setBusy(true);
    setRes(Object.fromEntries(suite.cases.map((c) => [c.id, "queued" as Result])));
    suite.cases.forEach((c, i) => {
      timers.current.push(setTimeout(() => setRes((r) => ({ ...r, [c.id]: "running" })), i * 600));
      timers.current.push(setTimeout(() => setRes((r) => ({ ...r, [c.id]: "pass" })), i * 600 + 520));
    });
    timers.current.push(setTimeout(() => { setBusy(false); toast(`${suite.name}: ${suite.cases.length}/${suite.cases.length} passed`); }, suite.cases.length * 600 + 200));
  };

  const maxCases = suite.cases.length;
  return (
    <section className="card rise" style={{ "--d": `${d}ms` } as React.CSSProperties}>
      <div className="ch">
        <div>
          <h2>{suite.name}</h2>
          <button type="button" className={s.agent} onClick={() => openAgent(agent.id)}><Icon name={agent.icon} size={13} />{agent.name}</button>
        </div>
        <div className="right">
          <span className={`tag ${pass === maxCases ? "ok" : "err"}`}>{pass}/{maxCases}</span>
          <button type="button" className="btn sm" onClick={run} disabled={busy}><Icon name="play" size={14} />{busy ? "Running" : "Run suite"}</button>
        </div>
      </div>
      <ul className={`${s.cases} rows`}>
        {suite.cases.map((c) => (
          <li key={c.id}>
            <span className={`${s.res} ${s[res[c.id]]}`}>{res[c.id] === "pass" ? <Icon name="check" size={12} strokeWidth={3} /> : res[c.id] === "fail" ? <Icon name="x" size={12} strokeWidth={3} /> : null}</span>
            <div style={{ minWidth: 0 }}><b>{c.name}</b><code>{c.check}</code></div>
          </li>
        ))}
      </ul>
      <div className={s.hist}>
        <small>by version</small>
        <div className={s.bars}>
          {suite.history.map((h) => (
            <div key={h.version} data-tip={`${h.version} · ${h.passed}/${maxCases} · ${h.at}`}>
              <i style={{ height: (h.passed / maxCases) * 36 }} className={h.passed === maxCases ? s.full : ""} />
              <span className="mono">{h.version}</span>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
