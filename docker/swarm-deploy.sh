#!/usr/bin/env bash
# Student Hub — full deploy, Swarm variant (docker/stack.yml). OPTIONAL: the shared VPS runs plain Docker Compose
# with Swarm inactive, and its host nginx owns ports 80/443. Use this only on a server prepared with
# docker/swarm-init.sh (docs/deployment/overview.md, "Swarm variant").
set -euo pipefail
cd "$(dirname "$0")/.."
FULL=1 MODE=swarm DEPLOY_NGINX=false exec bash docker/smart-deploy.sh "$@"
