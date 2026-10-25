import type { NextConfig } from "next";

const backendUrl = process.env.BACKEND_URL ?? "http://localhost:8000";
// The agent service binds 127.0.0.1, so the proxy must not depend on localhost resolving to IPv6.
const agentApiUrl = process.env.AGENT_API_URL ?? "http://127.0.0.1:8080";

const nextConfig: NextConfig = {
  async rewrites() {
    return [
      { source: "/api/:path*", destination: `${backendUrl}/api/:path*` },
      { source: "/agent-api/:path*", destination: `${agentApiUrl}/v1/:path*` },
    ];
  },
};

export default nextConfig;
