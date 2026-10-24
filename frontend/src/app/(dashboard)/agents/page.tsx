"use client";

import { PlusOutlined, RobotOutlined } from "@ant-design/icons";
import { Avatar, Button, Card, Col, Flex, Progress, Row, Segmented, Typography } from "antd";
import { useState } from "react";
import PageHeader from "@/components/PageHeader";
import StatusTag from "@/components/StatusTag";
import { agents, type AgentStatus } from "@/lib/mock-data";

type Filter = "all" | AgentStatus;

export default function AgentsPage() {
  const [filter, setFilter] = useState<Filter>("all");
  const visible = filter === "all" ? agents : agents.filter((a) => a.status === filter);

  return (
    <>
      <PageHeader
        title="Agents"
        subtitle="Build, deploy and monitor your agents."
        extra={<Button type="primary" icon={<PlusOutlined />}>New agent</Button>}
      />

      <Segmented<Filter>
        value={filter}
        onChange={setFilter}
        options={[
          { label: "All", value: "all" },
          { label: "Active", value: "active" },
          { label: "Paused", value: "paused" },
          { label: "Draft", value: "draft" },
        ]}
        style={{ marginBottom: 16 }}
      />

      <Row gutter={[16, 16]}>
        {visible.map((a) => (
          <Col key={a.id} xs={24} md={12} xl={8}>
            <Card variant="borderless" hoverable style={{ height: "100%" }}>
              <Flex justify="space-between" align="flex-start">
                <Avatar shape="square" size={40} icon={<RobotOutlined />} style={{ background: "#eef2ff", color: "#4f46e5" }} />
                <StatusTag status={a.status} />
              </Flex>
              <Typography.Title level={5} style={{ margin: "14px 0 4px" }}>
                {a.name}
              </Typography.Title>
              <Typography.Paragraph type="secondary" style={{ minHeight: 44 }}>
                {a.description}
              </Typography.Paragraph>
              <Flex justify="space-between" style={{ fontSize: 12, color: "#6b7280" }}>
                <span>{a.model}</span>
                <span>Updated {a.updated}</span>
              </Flex>
              <Flex align="center" gap={12} style={{ marginTop: 12 }}>
                <Progress percent={a.successRate} showInfo={false} size="small" style={{ margin: 0 }} />
                <span className="whitespace-nowrap text-xs text-gray-500">
                  {a.runs.toLocaleString()} runs
                </span>
              </Flex>
            </Card>
          </Col>
        ))}
      </Row>
    </>
  );
}
