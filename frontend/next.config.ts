import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // The default bottom-left dev badge covers the profile avatar in the rail.
  devIndicators: { position: "bottom-right" },
  async redirects() {
    // Pages folded into the agent, run and settings views keep working for old links.
    return [
      { source: "/runs", destination: "/dashboard", permanent: false },
      { source: "/runs/:id", destination: "/live-run/:id", permanent: false },
      { source: "/definitions", destination: "/agents/new", permanent: false },
      { source: "/org", destination: "/settings", permanent: false },
      { source: "/approvals", destination: "/dashboard", permanent: false },
      { source: "/guardrails", destination: "/agents", permanent: false },
      { source: "/prompts", destination: "/agents", permanent: false },
      { source: "/budgets", destination: "/agents", permanent: false },
      { source: "/triggers", destination: "/dashboard", permanent: false },
      { source: "/evaluate", destination: "/dashboard", permanent: false },
    ];
  },
  async rewrites() {
    const backendUrl = process.env.BACKEND_URL ?? "http://localhost:8000";
    return [
      { source: "/api/:path*", destination: `${backendUrl}/api/:path*` },
      { source: "/agent-api/:path*", destination: `${backendUrl}/agent-api/:path*` },
    ];
  },
};

export default nextConfig;
