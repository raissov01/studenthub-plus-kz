import type { NextConfig } from "next";

// The Go API (with its Postgres) runs on our own server; the site proxies /api/* to it so the
// browser sees one origin and the session cookie stays first-party.
// Local development: `go run ./cmd/server` on :8080.
const apiOrigin =
  process.env.API_ORIGIN ??
  (process.env.NODE_ENV === "development" ? "http://localhost:8080" : "https://studenthub.85-137-91-47.sslip.io");

// Keep the legacy Telegram login widget working: data-onauth is compiled with eval by
// telegram-widget.js. Inline scripts/styles are also needed by Next hydration and our boot theme.
// A nonce-only policy needs a separate rendering/widget migration; do not silently break sign-in.
const backend = new URL(apiOrigin).origin;
const wsOrigin = backend.replace(/^http/, 'ws');
const csp = [
  "default-src 'self'",
  "base-uri 'self'",
  "object-src 'none'",
  "form-action 'self'",
  "script-src 'self' 'unsafe-inline' 'unsafe-eval' https://telegram.org",
  "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
  "font-src 'self' https://fonts.gstatic.com",
  `img-src 'self' data: blob: ${backend} https://telegram.org https://oauth.telegram.org`,
  `connect-src 'self' ${backend} ${wsOrigin} https://studenthub.85-137-91-47.sslip.io wss://studenthub.85-137-91-47.sslip.io https://fonts.googleapis.com https://fonts.gstatic.com`,
  "frame-src https://telegram.org https://oauth.telegram.org",
  "worker-src 'self' blob:",
  "media-src 'self' blob:",
].join('; ');
// Telegram's official bridge trusts precisely https://web.telegram.org (both /a/ and /k/).
// All student routes can be deep links in the Mini App; public portfolios and admin cannot.
const miniAppCsp = `${csp}; frame-ancestors https://web.telegram.org`;
const standaloneCsp = `${csp}; frame-ancestors 'none'`;

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
