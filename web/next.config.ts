import type { NextConfig } from "next";

const API_TARGET = process.env.API_TARGET || "http://manager:8080";

const nextConfig: NextConfig = {
  async rewrites() {
    return [
      { source: "/api/:path*", destination: `${API_TARGET}/api/:path*` },
      { source: "/uploads/:path*", destination: `${API_TARGET}/uploads/:path*` },
    ];
  },
};
export default nextConfig;
