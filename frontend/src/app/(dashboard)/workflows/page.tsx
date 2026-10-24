"use client";

import { PlusOutlined } from "@ant-design/icons";
import { Button, Col, Row } from "antd";
import PageHeader from "@/components/PageHeader";
import WorkflowTile from "@/components/WorkflowTile";
import { workflows } from "@/lib/mock-data";

export default function WorkflowsPage() {
  return (
    <>
      <PageHeader
        title="Workflows"
        subtitle="One-tap automations that chain agents and tools together."
        extra={<Button type="primary" icon={<PlusOutlined />}>New workflow</Button>}
      />
      <Row gutter={[16, 16]}>
        {workflows.map((w) => (
          <Col key={w.id} xs={12} md={8} xl={6}>
            <WorkflowTile workflow={w} />
          </Col>
        ))}
      </Row>
    </>
  );
}
