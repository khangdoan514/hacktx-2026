"use client";

import { ArrowRightOutlined } from "@ant-design/icons";
import { Card, Col, Row, Statistic, Table, Typography } from "antd";
import Link from "next/link";
import PageHeader from "@/components/PageHeader";
import StatusTag from "@/components/StatusTag";
import WorkflowTile from "@/components/WorkflowTile";
import { agents, runs, workflows, type Run } from "@/lib/mock-data";

const stats = [
  { title: "Active agents", value: agents.filter((a) => a.status === "active").length },
  { title: "Runs today", value: 1248 },
  { title: "Success rate", value: 97.8, suffix: "%", precision: 1 },
  { title: "Avg. latency", value: 14.2, suffix: "s", precision: 1 },
];

const runColumns = [
  { title: "Run", dataIndex: "id", key: "id", render: (v: string) => <Typography.Text code>{v}</Typography.Text> },
  { title: "Agent", dataIndex: "agent", key: "agent" },
  { title: "Status", dataIndex: "status", key: "status", render: (s: Run["status"]) => <StatusTag status={s} /> },
  { title: "Duration", dataIndex: "duration", key: "duration" },
  { title: "Started", dataIndex: "started", key: "started" },
];

export default function OverviewPage() {
  return (
    <>
      <PageHeader title="Good afternoon" subtitle="Here's what your agents have been up to." />

      <Row gutter={[16, 16]}>
        {stats.map((s) => (
          <Col key={s.title} xs={12} lg={6}>
            <Card variant="borderless">
              <Statistic {...s} />
            </Card>
          </Col>
        ))}
      </Row>

      <Card
        variant="borderless"
        title="Quick workflows"
        extra={<Link href="/workflows">View all <ArrowRightOutlined /></Link>}
        style={{ marginTop: 16 }}
      >
        <Row gutter={[12, 12]}>
          {workflows.slice(0, 4).map((w) => (
            <Col key={w.id} xs={12} md={6}>
              <WorkflowTile workflow={w} />
            </Col>
          ))}
        </Row>
      </Card>

      <Card
        variant="borderless"
        title="Recent runs"
        extra={<Link href="/runs">View all <ArrowRightOutlined /></Link>}
        style={{ marginTop: 16 }}
        styles={{ body: { padding: 0 } }}
      >
        <Table rowKey="id" columns={runColumns} dataSource={runs.slice(0, 5)} pagination={false} />
      </Card>
    </>
  );
}
