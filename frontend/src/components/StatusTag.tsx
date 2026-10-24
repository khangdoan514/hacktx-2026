import { Tag } from "antd";
import type { AgentStatus, RunStatus } from "@/lib/mock-data";

const colors: Record<AgentStatus | RunStatus, string> = {
  active: "success",
  succeeded: "success",
  running: "processing",
  paused: "warning",
  failed: "error",
  draft: "default",
};

export default function StatusTag({ status }: { status: AgentStatus | RunStatus }) {
  return (
    <Tag color={colors[status]} variant="filled" style={{ textTransform: "capitalize" }}>
      {status}
    </Tag>
  );
}
