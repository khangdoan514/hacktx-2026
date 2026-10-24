"use client";

import { Badge } from "antd";
import { useEffect, useState } from "react";
import { agentApi } from "@/lib/api";

type State = "checking" | "online" | "offline";
const display: Record<State, { status: "processing" | "success" | "error"; text: string }> = {
  checking: { status: "processing", text: "Checking agent API" },
  online: { status: "success", text: "Agent API online" },
  offline: { status: "error", text: "Agent API offline" },
};
export default function BackendStatus() {
  const [state, setState] = useState<State>("checking");
  useEffect(() => { agentApi.agents().then(() => setState("online")).catch(() => setState("offline")); }, []);
  return <Badge status={display[state].status} text={display[state].text} />;
}
