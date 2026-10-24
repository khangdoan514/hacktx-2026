"use client";

import RunInspector from "@/components/run/RunInspector";
import { useRunView } from "@/lib/store";

export default function LiveRunPage() {
  const view = useRunView();
  return view ? <RunInspector view={view} /> : null;
}
