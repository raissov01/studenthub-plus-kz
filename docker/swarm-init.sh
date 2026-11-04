#!/usr/bin/env bash
# Student Hub — one-time Swarm initialisation for the OPTIONAL Swarm variant. Runs on the server:
#   ssh root@<server> 'bash -s' < docker/swarm-init.sh
# Not for the shared VPS as it is today (Swarm inactive there, host nginx on 80/443, other projects' containers).
set -euo pipefail
APP_ROOT="${APP_ROOT:-/opt/studenthub}"

echo "=== Docker Swarm ==="
docker swarm init 2>/dev/null || echo "Swarm already initialised"

echo ""
echo "=== Stop the Compose project if it runs (the volume studenthub-pg is kept) ==="
if [ -f "$APP_ROOT/docker/docker-compose.yml" ]; then
  docker compose -f "$APP_ROOT/docker/docker-compose.yml" down --remove-orphans || true
fi

echo ""
echo "=== Secrets ==="
if ! docker secret inspect studenthub_postgres_password >/dev/null 2>&1; then
  grep -E '^POSTGRES_PASSWORD=' "$APP_ROOT/docker/.env" | cut -d= -f2- | tr -d '\n' |
    docker secret create studenthub_postgres_password -
  echo "created studenthub_postgres_password from $APP_ROOT/docker/.env"
else
  echo "studenthub_postgres_password exists"
fi

echo ""
echo "=== Ready. From the laptop: bash docker/swarm-deploy.sh ==="
