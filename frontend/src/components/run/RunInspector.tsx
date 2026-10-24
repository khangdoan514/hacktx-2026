"use client";

import Link from "next/link";
import { useState } from "react";
import { agentById } from "@/config/agents";
import PageHeader from "@/components/shell/PageHeader";
import Icon from "@/components/ui/Icon";
import StatePill from "@/components/ui/StatePill";
import { clock, compact, usd } from "@/lib/format";
import { useAllRuns, useStore } from "@/lib/store";
import type { RunView } from "@/lib/types";
import ExecutionGraph from "./ExecutionGraph";
import LimitMeters from "./LimitMeters";
import NewRunDialog from "./NewRunDialog";
import Output, { OutputBadge } from "./Output";
import RunList from "./RunList";
import Timeline from "./Timeline";
import s from "./RunInspector.module.css";

/** One run, drawn literally: graph, lanes, output, plus the runs around it. */
export default function RunInspector({ view }: { view: RunView }) {
  const { live, cancelLive } = useStore();
  const runs = useAllRuns();
  const [dialog, setDialog] = useState(false);
  const def = agentById(view.plan.agent)!;
  const isLive = live?.id === view.plan.id;
  const p = view.plan;

  return (
    <>
      <PageHeader
        crumbs={<><Link href="/runs">Runs</Link><span>/</span><b className={s.crumbId}>{p.id}</b></>}
        title={<><span>{def.name}</span><StatePill state={view.state} big /></>}
        sub={p.task}
        code={{
          cli: `nmc3 run ${def.id} \\\n  --task "${p.task}" \\\n  --max-turns ${p.limits.turns} --max-tools ${p.limits.tools} --timeout ${p.limits.seconds}s\n\nnmc3 runs watch ${p.id}`,
          sdk: `import { NMC3 } from "@nmc3/sdk";\n\nconst nmc3 = new NMC3();\nconst run = await nmc3.runs.start({\n  agent: "${def.id}@${def.version}",\n  input: { task: "${p.task}" },\n  limits: ${JSON.stringify(p.limits)},\n});\n\nfor await (const event of nmc3.runs.events(run.id)) console.log(event.type);`,
        }}
        actions={
          <>
            <span className={s.clock}>{clock(view.elapsed)}</span>
            {isLive && view.state === "RUNNING" && <button type="button" className="btn" onClick={cancelLive}><Icon name="square" size={15} />Cancel</button>}
            <button type="button" className="btn solid" onClick={() => setDialog(true)}><Icon name="play" size={15} />New run</button>
          </>
        }
      />

      <section className="card rise" style={{ "--d": "60ms" } as React.CSSProperties}>
        <div className="ch">
          <h2>How this run is executing</h2>
          <span className="n">{compact(view.tokens)} tokens · {usd(view.cost)} · {p.trigger}</span>
          <LimitMeters used={view.used} limits={p.limits} />
        </div>
        <ExecutionGraph view={view} />
        <Timeline view={view} />
      </section>

      <div className={s.lower}>
        <section className="card rise" style={{ "--d": "120ms" } as React.CSSProperties}>
          <div className="ch"><h2>Output</h2><OutputBadge view={view} /></div>
          <Output view={view} />
        </section>
        <section className="card rise" style={{ "--d": "160ms" } as React.CSSProperties}>
          <div className="ch"><h2>Runs</h2><span className="n">{runs.length}</span><Link href="/runs" className="right btn ghost sm">All</Link></div>
          <RunList runs={runs.slice(0, 6)} current={p.id} />
        </section>
      </div>

      <NewRunDialog open={dialog} onClose={() => setDialog(false)} agent={p.agent} />
    </>
  );
}
