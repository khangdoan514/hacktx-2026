"use client";

import Link from "next/link";
import { useState } from "react";
import PageHeader from "@/components/shell/PageHeader";
import Icon from "@/components/ui/Icon";
import StatePill from "@/components/ui/StatePill";
import { clock, compact, usd } from "@/lib/format";
import { isTerminal } from "@/lib/live";
import { useAllRuns, useStore } from "@/lib/store";
import type { RunView } from "@/lib/types";
import ExecutionGraph from "./ExecutionGraph";
import LimitMeters from "./LimitMeters";
import NewRunDialog from "./NewRunDialog";
import Output, { OutputBadge } from "./Output";
import RunList from "./RunList";
import Timeline from "./Timeline";
import s from "./RunInspector.module.css";

/** One run, drawn from what the service recorded: graph, lanes, output, plus the runs around it. */
export default function RunInspector({ view }: { view: RunView }) {
  const { cancelRun, agentById, toast } = useStore();
  const runs = useAllRuns();
  const [dialog, setDialog] = useState(false);
  const p = view.plan;
  const def = agentById(p.agent);

  return (
    <>
      <PageHeader
        crumbs={<><Link href="/runs">Runs</Link><span>/</span><b className={s.crumbId}>{p.id}</b></>}
        title={<><span>{def?.name ?? p.agent}</span><StatePill state={view.state} big /></>}
        sub={p.task}
        actions={
          <>
            <span className={s.clock}>{clock(view.elapsed)}</span>
            {!isTerminal(view.state) && (
              <button type="button" className="btn" onClick={() => { void cancelRun(p.id).catch((e: Error) => toast(e.message)); }}><Icon name="square" size={15} />Cancel</button>
            )}
            <button type="button" className="btn solid" onClick={() => setDialog(true)}><Icon name="play" size={15} />New run</button>
          </>
        }
      />

      <section className="card rise" style={{ "--d": "60ms" } as React.CSSProperties}>
        <div className="ch">
          <h2>How this run executed</h2>
          <span className="n">{compact(view.tokens || p.usage.tokensIn + p.usage.tokensOut)} tokens · {usd(view.cost || p.usage.cost)}{p.model ? ` · ${p.model}` : ""}</span>
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
