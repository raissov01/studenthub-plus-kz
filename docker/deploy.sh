#!/usr/bin/env bash
# Student Hub — FULL deploy to the shared VPS (Docker Compose): build both images here, ship them, run migrations,
# re-apply compose, host nginx and timers, health-check https://studenthub.studywithraissov.com/api/health.
# Same engine as the smart deploy, with every bucket forced on.
#
#   bash docker/deploy.sh
#   SWITCH_FROM_SYSTEMD=1 bash docker/deploy.sh   # the one-time switch from the old systemd setup (docs/deployment/vps.md)
set -euo pipefail
cd "$(dirname "$0")/.."
FULL=1 MODE=compose exec bash docker/smart-deploy.sh "$@"
