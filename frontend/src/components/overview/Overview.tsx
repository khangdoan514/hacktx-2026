"use client";

import { useFlowSimulation } from "@/lib/flows/useFlowSimulation";
import { useStore } from "@/lib/store";
import BriefForm from "./BriefForm";
import DecisionTimeline from "./DecisionTimeline";
import KpiStrip from "./KpiStrip";
import Sidebar from "./Sidebar";
import StepsBoard from "./StepsBoard";
import TopBar from "./TopBar";
import { usePendingApprovals } from "./pending";
import s from "./overview.module.css";

export default function Overview() {
  const { toast } = useStore();
  const flow = useFlowSimulation(toast);
  const approvals = usePendingApprovals();
  const waiting = flow.waiting + approvals.items.length;

  return (
    <div className={s.app}>
      <Sidebar waiting={waiting} />
      <div className={s.main}>
        <TopBar source={flow.source} />
        <main className={s.page}>
          <header className={`${s.head} rise`}>
            <h1>Overview</h1>
            <p>Every flow your agents are running, what they decided, and what is waiting on you.</p>
            <BriefForm onToast={toast} onStarted={() => void flow.refresh()} />
          </header>
          <KpiStrip flow={flow} waiting={waiting} />
          <StepsBoard lanes={flow.lanes} />
          <DecisionTimeline flow={flow} pending={approvals.items} approvalsMounted={approvals.mounted} decide={approvals.decide} onToast={toast} />
        </main>
      </div>
    </div>
  );
}
