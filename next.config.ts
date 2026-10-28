import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // In development the Go API runs separately (go run ./cmd/server); on Vercel,
  // vercel.json sends /api/* to the Go function instead.
  async rewrites() {
    if (process.env.NODE_ENV !== "development") return [];
    return [{ source: "/api/:path*", destination: `${process.env.API_ORIGIN ?? "http://localhost:8080"}/api/:path*` }];
  },
};

export default nextConfig;
