"use client";

import { useState } from "react";
import NewRunDialog from "@/components/run/NewRunDialog";
import RunInspector from "@/components/run/RunInspector";
import PageHeader from "@/components/shell/PageHeader";
import Icon from "@/components/ui/Icon";
import { useRunView } from "@/lib/store";

/** The run in flight, or the latest one the agent service has. */
export default function LiveRunPage() {
  const view = useRunView();
  const [dialog, setDialog] = useState(false);
  if (view === undefined) return null;
  if (view === null) {
    return (
      <>
        <PageHeader title="Live run" sub="The agent service has no runs yet." actions={<button type="button" className="btn solid" onClick={() => setDialog(true)}><Icon name="play" size={15} />New run</button>} />
        <NewRunDialog open={dialog} onClose={() => setDialog(false)} />
      </>
    );
  }
  return <RunInspector view={view} />;
}
