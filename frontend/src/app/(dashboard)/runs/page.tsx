"use client";

import { Card, Table, Typography } from "antd";
import PageHeader from "@/components/PageHeader";
import StatusTag from "@/components/StatusTag";
import { runs, type Run } from "@/lib/mock-data";

const columns = [
  { title: "Run", dataIndex: "id", key: "id", render: (v: string) => <Typography.Text code>{v}</Typography.Text> },
  { title: "Agent", dataIndex: "agent", key: "agent" },
  { title: "Trigger", dataIndex: "trigger", key: "trigger" },
  {
    title: "Status",
    dataIndex: "status",
    key: "status",
    render: (s: Run["status"]) => <StatusTag status={s} />,
    filters: ["succeeded", "running", "failed"].map((s) => ({ text: s, value: s })),
    onFilter: (value: React.Key | boolean, record: Run) => record.status === value,
  },
  { title: "Duration", dataIndex: "duration", key: "duration" },
  { title: "Started", dataIndex: "started", key: "started" },
];

export default function RunsPage() {
  return (
    <>
      <PageHeader title="Runs" subtitle="Every agent execution, newest first." />
      <Card variant="borderless" styles={{ body: { padding: 0 } }}>
        <Table rowKey="id" columns={columns} dataSource={runs} />
      </Card>
    </>
  );
}
