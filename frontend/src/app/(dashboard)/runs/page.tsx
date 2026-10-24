"use client";

import { Alert, Button, Card, Col, Flex, Input, Row, Space, Spin, Tag, Timeline, Typography } from "antd";
import { useCallback, useEffect, useState } from "react";
import PageHeader from "@/components/PageHeader";
import { agentApi, type AgentRun, type RunEvent, type StartRequest } from "@/lib/api";

const example: StartRequest = {
  agent_id: "vendor-orchestrator", agent_version: "1.0.0",
  input: { task: "Compare fictional vendors for a small support team", criteria: ["price", "accessibility"], budget_usd: 1000 },
  limits: { max_delegations: 2, max_concurrent_agents: 2, max_tool_calls: 4,
    max_model_turns: 8, max_tokens: 12000, max_cost_usd: 2, timeout_seconds: 60 },
};
const terminal = new Set(["SUCCEEDED", "FAILED", "CANCELLED"]);
const pretty = (value: unknown) => JSON.stringify(value, null, 2);

export default function RunsPage() {
  const [requestText, setRequestText] = useState(pretty(example));
  const [runId, setRunId] = useState("");
  const [lookupId, setLookupId] = useState("");
  const [run, setRun] = useState<AgentRun | null>(null);
  const [events, setEvents] = useState<RunEvent[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const runState = run?.state;

  const refresh = useCallback(async (id: string) => {
    try {
      const [snapshot, history] = await Promise.all([agentApi.run(id), agentApi.events(id)]);
      setRun(snapshot.run);
      setEvents(history.events);
      setError("");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not load the run");
    }
  }, []);

  useEffect(() => {
    if (!runId) return;
    void refresh(runId);
    if (terminal.has(runState ?? "")) return;
    const timer = window.setInterval(() => { void refresh(runId); }, 1000);
    return () => window.clearInterval(timer);
  }, [runId, runState, refresh]);

  async function start() {
    setError(""); setLoading(true); setRun(null); setEvents([]);
    try {
      const request = JSON.parse(requestText) as StartRequest;
      const response = await agentApi.start(request);
      setRunId(response.run_id);
      setLookupId(response.run_id);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not start the run");
    } finally { setLoading(false); }
  }

  function inspect() {
    const id = lookupId.trim();
    if (!id) return;
    setError(""); setRun(null); setEvents([]); setRunId(id);
    void refresh(id);
  }

  return <>
    <PageHeader title="Agent runs" subtitle="Start a run and inspect its persisted history." />
    <Row gutter={[16, 16]}>
      <Col xs={24} lg={12}>
        <Card title="Start a run" variant="borderless">
          <Typography.Paragraph type="secondary">Edit the versioned request JSON, then submit it to the Go agent API.</Typography.Paragraph>
          <Input.TextArea aria-label="Run request JSON" value={requestText} onChange={(event) => setRequestText(event.target.value)} autoSize={{ minRows: 15, maxRows: 22 }} style={{ fontFamily: "monospace" }} />
          <Button type="primary" onClick={() => void start()} loading={loading} style={{ marginTop: 12 }}>Start run</Button>
        </Card>
      </Col>
      <Col xs={24} lg={12}>
        <Card title="Inspect a run" variant="borderless">
          <Space.Compact style={{ width: "100%" }}>
            <Input aria-label="Run ID" placeholder="Run ID" value={lookupId} onChange={(event) => setLookupId(event.target.value)} onPressEnter={inspect} />
            <Button onClick={inspect}>Load</Button>
          </Space.Compact>
          {error && <Alert type="error" showIcon message={error} style={{ marginTop: 16 }} />}
          {runId && !run && !error && <Flex align="center" gap={10} style={{ marginTop: 16 }}><Spin size="small" /> Loading run…</Flex>}
          {run && <div style={{ marginTop: 20 }}>
            <Flex align="center" gap={8} wrap><Typography.Text code>{run.id}</Typography.Text><Tag color={run.state === "SUCCEEDED" ? "success" : run.state === "FAILED" ? "error" : "processing"}>{run.state}</Tag></Flex>
            {run.error && <Alert type="error" showIcon message={`${run.error.code}: ${run.error.message}`} style={{ marginTop: 12 }} />}
            <Typography.Paragraph style={{ marginTop: 12 }}>Usage: {run.usage?.input_tokens ?? 0} input tokens, {run.usage?.output_tokens ?? 0} output tokens, ${(run.usage?.cost_usd ?? 0).toFixed(4)}</Typography.Paragraph>
            {run.output !== undefined && <><Typography.Title level={5}>Result</Typography.Title><pre style={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>{pretty(run.output)}</pre></>}
          </div>}
        </Card>
      </Col>
    </Row>
    {run && <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
      <Col xs={24} lg={12}><Card title="Invocation tree" variant="borderless">
        {run.invocations?.length ? run.invocations.map((invocation) => <div key={invocation.id} style={{ marginLeft: invocation.depth * 24, padding: "9px 0", borderBottom: "1px solid #eee" }}>
          <Flex gap={8} align="center" wrap><Typography.Text strong>{invocation.agent_id}</Typography.Text><Tag>{invocation.state}</Tag></Flex>
          <Typography.Text type="secondary">{invocation.id} · depth {invocation.depth}</Typography.Text>
          {invocation.error && <Alert type="error" message={`${invocation.error.code}: ${invocation.error.message}`} style={{ marginTop: 8 }} />}
          {invocation.output !== undefined && <pre style={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>{pretty(invocation.output)}</pre>}
        </div>) : <Typography.Text type="secondary">Waiting for invocations…</Typography.Text>}
      </Card></Col>
      <Col xs={24} lg={12}><Card title="Ordered events" variant="borderless">
        <Timeline items={events.map((event) => ({ children: <div key={event.sequence}>
          <Typography.Text strong>#{event.sequence} {event.type}</Typography.Text><br />
          <Typography.Text type="secondary">{event.invocation_id ?? "run"} · {new Date(event.timestamp).toLocaleTimeString()}</Typography.Text>
          {event.data !== undefined && <pre style={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>{pretty(event.data)}</pre>}
        </div> }))} />
        {!events.length && <Typography.Text type="secondary">Waiting for events…</Typography.Text>}
      </Card></Col>
    </Row>}
    {run?.tool_invocations?.length ? <Card title="Tool outcomes" variant="borderless" style={{ marginTop: 16 }}>
      {run.tool_invocations.map((tool) => <div key={tool.id} style={{ padding: "9px 0", borderBottom: "1px solid #eee" }}>
        <Typography.Text strong>{tool.tool_id}@{tool.tool_version}</Typography.Text>
        <Typography.Text type="secondary"> · {tool.agent_invocation_id}</Typography.Text>
        {tool.error && <Alert type="error" message={`${tool.error.code}: ${tool.error.message}`} style={{ marginTop: 8 }} />}
        {tool.output !== undefined && <pre style={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>{pretty(tool.output)}</pre>}
      </div>)}
    </Card> : null}
  </>;
}
