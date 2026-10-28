import type { NextConfig } from "next";

// The Go API (with its Postgres) runs on our own server; the site proxies /api/* to it so the
// browser sees one origin and the session cookie stays first-party.
// Local development: `go run ./cmd/server` on :8080.
const apiOrigin =
  process.env.API_ORIGIN ??
  (process.env.NODE_ENV === "development" ? "http://localhost:8080" : "https://studenthub.85-137-91-47.sslip.io");

// Offline support (docs/PWA.md): every build gets its own id; the page registers /sw.js?v=<id>, so a deploy installs
// a new service worker with fresh caches and the student sees «Жаңа нұсқа дайын — Жаңарту».
const buildId = (process.env.VERCEL_GIT_COMMIT_SHA || "").slice(0, 12) || Date.now().toString(36);

const nextConfig: NextConfig = {
  env: { NEXT_PUBLIC_BUILD_ID: buildId },
  async rewrites() {
    return [{ source: "/api/:path*", destination: `${apiOrigin}/api/:path*` }];
  },
  async headers() {
    return [
      {
        // the worker script itself must never come from a cache, or updates would be missed
        source: "/sw.js",
        headers: [
          { key: "Content-Type", value: "application/javascript; charset=utf-8" },
          { key: "Cache-Control", value: "no-cache, no-store, must-revalidate" },
          { key: "Service-Worker-Allowed", value: "/" },
        ],
      },
      { source: "/offline.html", headers: [{ key: "Cache-Control", value: "no-cache" }] },
    ];
  },
};

export default nextConfig;
