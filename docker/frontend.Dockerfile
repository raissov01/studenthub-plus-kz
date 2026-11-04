# syntax=docker/dockerfile:1.7
# Student Hub web (Next.js, output: 'standalone'). Build context: frontend/
#   docker build -f docker/frontend.Dockerfile --build-arg BUILD_ID=$(git rev-parse --short=12 HEAD) \
#     -t studenthub-web:<sha> frontend/
# The app shell and every tab are prerendered here, so the running server mostly hands out static files.
FROM node:22-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json ./
# no browsers in the image: Playwright is a dev dependency, its download is skipped
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund

FROM node:22-alpine AS builder
WORKDIR /app
# NEXT_PUBLIC_* are inlined into the client bundle; API_ORIGIN fixes the /api rewrite (used only when nginx is not
# in front); BUILD_ID names the service worker version (the git sha)
ARG NEXT_PUBLIC_SITE_URL=https://studenthub.studywithraissov.com
ARG API_ORIGIN=http://api:8110
ARG BUILD_ID=dev
ENV NEXT_PUBLIC_SITE_URL=$NEXT_PUBLIC_SITE_URL API_ORIGIN=$API_ORIGIN BUILD_ID=$BUILD_ID \
    NEXT_TELEMETRY_DISABLED=1 NODE_OPTIONS=--max-old-space-size=2048
COPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN --mount=type=cache,target=/app/.next/cache npm run build

FROM node:22-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 HOSTNAME=0.0.0.0 PORT=8112 \
    API_ORIGIN=http://api:8110 NODE_OPTIONS=--max-old-space-size=192
ARG BUILD_ID=dev
LABEL org.opencontainers.image.title="studenthub-web" org.opencontainers.image.revision=$BUILD_ID
# the standalone server, then the files it does not copy itself (public/ and the fingerprinted build files)
COPY --from=builder --chown=node:node /app/.next/standalone ./
COPY --from=builder --chown=node:node /app/.next/static ./.next/static
COPY --from=builder --chown=node:node /app/public ./public
USER node
EXPOSE 8112
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD node -e "fetch('http://127.0.0.1:8112/manifest.webmanifest').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
CMD ["node", "server.js"]
