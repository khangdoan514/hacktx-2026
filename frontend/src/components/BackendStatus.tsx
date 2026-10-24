"use client";

import { Badge } from "antd";
import { useEffect, useState } from "react";
import { apiGet } from "@/lib/api";

type State = "checking" | "online" | "offline";

const display: Record<State, { status: "processing" | "success" | "error"; text: string }> = {
  checking: { status: "processing", text: "Checking API" },
  online: { status: "success", text: "API online" },
  offline: { status: "error", text: "API offline" },
};

export default function BackendStatus() {
  const [state, setState] = useState<State>("checking");

  useEffect(() => {
    apiGet<{ status: string }>("/health")
      .then(() => setState("online"))
      .catch(() => setState("offline"));
  }, []);

  return <Badge status={display[state].status} text={display[state].text} />;
}
