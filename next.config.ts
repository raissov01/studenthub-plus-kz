import type { NextConfig } from "next";

// The Go API (with its Postgres) runs on our own server; the site proxies /api/* to it so the
// browser sees one origin and the session cookie stays first-party.
// Local development: `go run ./cmd/server` on :8080.
const apiOrigin =
  process.env.API_ORIGIN ??
  (process.env.NODE_ENV === "development" ? "http://localhost:8080" : "https://studenthub.85-137-91-47.sslip.io");

const nextConfig: NextConfig = {
  async rewrites() {
    return [{ source: "/api/:path*", destination: `${apiOrigin}/api/:path*` }];
  },
};

export default nextConfig;
