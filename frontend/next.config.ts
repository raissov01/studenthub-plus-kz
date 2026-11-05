import type { NextConfig } from "next";

// Production (docs/architecture.md): the host nginx sends /api/* straight to the Go API, so the browser sees one
// origin and the session cookie stays first-party. The rewrite below only serves `next dev`, the Playwright fixture
// server and a bare `next start` without nginx in front. Rewrites are fixed at build time: the image build passes
// API_ORIGIN=http://api:8110 (the compose service). Server components read API_ORIGIN at run time (lib/content.ts).
// Local development: `cd backend && go run ./cmd/server` on :8080.
const apiOrigin =
  process.env.API_ORIGIN ??
  (process.env.NODE_ENV === "development" ? "http://localhost:8080" : "https://studenthub.studywithraissov.com");

const siteUrl = new URL(process.env.NEXT_PUBLIC_SITE_URL || "https://studenthub.studywithraissov.com");
const siteWs = `wss://${siteUrl.host}`;

// Keep the legacy Telegram login widget working: data-onauth is compiled with eval by
// telegram-widget.js. Inline scripts/styles are also needed by Next hydration and our boot theme.
// A nonce-only policy needs a separate rendering/widget migration; do not silently break sign-in.
// connect-src: the API and the quiz WebSocket are same-origin ('self' + wss://<site>; some browsers do not let
// 'self' cover wss:). In `next dev` the quiz socket goes to the Go dev server directly.
const devWs = process.env.NODE_ENV === "development" ? " ws://localhost:8080" : "";
const csp = [
  "default-src 'self'",
  "base-uri 'self'",
  "object-src 'none'",
  "form-action 'self'",
  "script-src 'self' 'unsafe-inline' 'unsafe-eval' https://telegram.org",
  "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
  "font-src 'self' https://fonts.gstatic.com",
  "img-src 'self' data: blob: https://telegram.org https://oauth.telegram.org",
  `connect-src 'self' ${siteWs}${devWs} https://fonts.googleapis.com https://fonts.gstatic.com`,
  // blob: = the in-app certificate viewer (a PDF the student picked or downloaded with the session, typed application/pdf)
  "frame-src https://telegram.org https://oauth.telegram.org blob:",
  "worker-src 'self' blob:",
  "media-src 'self' blob:",
].join('; ');
// Telegram's official bridge trusts precisely https://web.telegram.org (both /a/ and /k/).
// All student routes can be deep links in the Mini App; public portfolios and admin cannot.
const miniAppCsp = `${csp}; frame-ancestors https://web.telegram.org`;
const standaloneCsp = `${csp}; frame-ancestors 'none'`;

// Offline support (docs/PWA.md): every build gets its own id; the page registers /sw.js?v=<id>, so a deploy installs
// a new service worker with fresh caches and the student sees «Жаңа нұсқа дайын — Жаңарту».
// BUILD_ID is the git sha passed as a docker build arg (docker/deploy.sh); a local build falls back to the time.
const buildId = (process.env.BUILD_ID || process.env.GIT_SHA || "").slice(0, 12) || Date.now().toString(36);

const nextConfig: NextConfig = {
  // a self-contained server (node server.js) for the web image, docker/frontend.Dockerfile
  output: "standalone",
  // next/image is not used (plain <img>: blob: URLs and API photos), so the image optimizer and its sharp binaries
  // (~37 MB) stay out of the image
  images: { unoptimized: true },
  outputFileTracingExcludes: { "*": ["node_modules/@img/**", "node_modules/sharp/**"] },
  env: { NEXT_PUBLIC_BUILD_ID: buildId },
  async rewrites() {
    return [{ source: "/api/:path*", destination: `${apiOrigin}/api/:path*` }];
  },
  async headers() {
    return [
      {
        source: '/:path*',
        headers: [
          { key: 'Content-Security-Policy', value: miniAppCsp },
          { key: 'Referrer-Policy', value: 'strict-origin-when-cross-origin' },
          { key: 'Permissions-Policy', value: 'geolocation=(self), camera=(self), microphone=(), payment=(), usb=(), serial=(), accelerometer=(), gyroscope=(), magnetometer=()' },
          { key: 'X-Content-Type-Options', value: 'nosniff' },
        ],
      },
      ...['/admin/:path*', '/p/:path*'].map(source => ({
        source,
        headers: [
          { key: 'Content-Security-Policy', value: standaloneCsp },
          { key: 'X-Frame-Options', value: 'DENY' },
        ],
      })),
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
