import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  async rewrites() {
    const backendUrl = process.env.BACKEND_URL ?? "http://localhost:8000";
    return [
      { source: "/api/:path*", destination: `${backendUrl}/api/:path*` },
      { source: "/agent-api/:path*", destination: `${backendUrl}/agent-api/:path*` },
    ];
  },
};

export default nextConfig;
