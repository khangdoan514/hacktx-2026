"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import RunInspector from "@/components/run/RunInspector";
import { useRunView } from "@/lib/store";

export default function RunPage() {
  const { id } = useParams<{ id: string }>();
  const view = useRunView(id);
  if (view === undefined) return null;
  if (view === null) {
    return (
      <div className="card" style={{ padding: 28 }}>
        <p className="mono c-err">NOT_FOUND</p>
        <p className="muted" style={{ marginTop: 6 }}>No run {id}. <Link href="/runs" className="c-run">All runs</Link></p>
      </div>
    );
  }
  return <RunInspector view={view} />;
}
