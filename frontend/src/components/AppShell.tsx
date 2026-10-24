"use client";

import {
  AppstoreOutlined,
  HistoryOutlined,
  HomeOutlined,
  PlusOutlined,
  RobotOutlined,
  SearchOutlined,
} from "@ant-design/icons";
import { Avatar, Button, Flex, Input, Layout, Menu } from "antd";
import Link from "next/link";
import { usePathname } from "next/navigation";
import BackendStatus from "./BackendStatus";

const { Sider, Header, Content } = Layout;

const nav = [
  { key: "/", icon: <HomeOutlined />, label: "Overview" },
  { key: "/agents", icon: <RobotOutlined />, label: "Agents" },
  { key: "/workflows", icon: <AppstoreOutlined />, label: "Workflows" },
  { key: "/runs", icon: <HistoryOutlined />, label: "Runs" },
];

export default function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const selected = nav.find((n) => n.key !== "/" && pathname.startsWith(n.key))?.key ?? "/";

  return (
    <Layout style={{ minHeight: "100vh" }}>
      <Sider
        width={232}
        breakpoint="lg"
        collapsedWidth={0}
        style={{ borderRight: "1px solid #eceef2" }}
      >
        <Flex align="center" gap={10} style={{ height: 60, padding: "0 20px" }}>
          <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-indigo-600 text-sm font-bold text-white">
            H
          </div>
          <span className="text-[15px] font-semibold">HackTX</span>
        </Flex>
        <Menu
          mode="inline"
          selectedKeys={[selected]}
          style={{ borderInlineEnd: "none" }}
          items={nav.map((n) => ({
            key: n.key,
            icon: n.icon,
            label: <Link href={n.key}>{n.label}</Link>,
          }))}
        />
      </Sider>
      <Layout>
        <Header style={{ borderBottom: "1px solid #eceef2" }}>
          <Flex align="center" justify="space-between" gap={16} style={{ height: "100%" }}>
            <Input
              prefix={<SearchOutlined style={{ color: "#9ca3af" }} />}
              placeholder="Search agents, workflows, runs…"
              variant="filled"
              style={{ maxWidth: 360 }}
            />
            <Flex align="center" gap={20}>
              <BackendStatus />
              <Button type="primary" icon={<PlusOutlined />}>
                New agent
              </Button>
              <Avatar style={{ background: "#e0e7ff", color: "#4338ca" }}>K</Avatar>
            </Flex>
          </Flex>
        </Header>
        <Content style={{ padding: 28 }}>
          <div className="mx-auto max-w-[1200px]">{children}</div>
        </Content>
      </Layout>
    </Layout>
  );
}
