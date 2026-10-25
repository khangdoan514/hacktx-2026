import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // The default bottom-left dev badge covers the profile avatar in the rail.
  devIndicators: { position: "bottom-right" },
  async redirects() {
    return [
      { source: "/", destination: "/dashboard", permanent: false },
      { source: "/runs", destination: "/dashboard", permanent: false },
      { source: "/runs/:id", destination: "/live-run/:id", permanent: false },
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
