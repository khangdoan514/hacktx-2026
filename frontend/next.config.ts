import type { NextConfig } from "next";

const backendUrl = process.env.BACKEND_URL ?? "http://localhost:8000";
const agentApiUrl = process.env.AGENT_API_URL ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  async rewrites() {
    return [
      { source: "/api/:path*", destination: `${backendUrl}/api/:path*` },
      { source: "/agent-api/:path*", destination: `${agentApiUrl}/v1/:path*` },
    ];
  },
};

export default nextConfig;
