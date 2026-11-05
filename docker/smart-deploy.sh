#!/usr/bin/env bash
# Student Hub — smart deploy from a LAPTOP to the shared VPS. The VPS never builds (its RAM is shared with client
# bots): images are built here, streamed over ssh with `docker save | docker load`, then docker/vps-apply.sh
# restarts only what changed.
#
#   bash docker/smart-deploy.sh                 # diff against the sha deployed on the server, deploy only that
#   DRY_RUN=1 bash docker/smart-deploy.sh       # print the decision, touch nothing (reads DEPLOYED_SHA over ssh)
#   OFFLINE=1 BASE=<sha> DRY_RUN=1 ...          # the same without ssh
#   FULL=1 bash docker/smart-deploy.sh          # everything (= bash docker/deploy.sh)
#   DEPLOY_FRONTEND=true bash docker/smart-deploy.sh   # force a bucket (skips the detection)
#   MODE=swarm ...                              # the optional Swarm variant (docker/stack.yml)
#
# What changed is decided by scripts/detect-smart-deploy.sh (docs/deployment/smart-deploy.md):
#   frontend -> build + ship + restart web; backend -> api; migrations -> /app/migrate before api starts;
#   docker-compose.yml / stack.yml -> re-apply; nginx.host.conf -> host nginx; docker/systemd -> timers;
#   services/kaspi-pos -> build + ship + restart the café sidecar (only where COMPOSE_PROFILES has cafe-kaspi);
#   docs / tests only -> nothing at all.
set -euo pipefail
cd "$(dirname "$0")/.."

VPS="${VPS:-root@85.137.91.47}"
APP_ROOT="${APP_ROOT:-/opt/studenthub}"
SITE_URL="${SITE_URL:-https://studenthub.studywithraissov.com}"
PLATFORM="${PLATFORM:-linux/amd64}"
MODE="${MODE:-compose}"
DRY_RUN="${DRY_RUN:-0}"
FULL="${FULL:-0}"

bool() { case "${1:-false}" in 1|true|TRUE|yes|on) return 0 ;; *) return 1 ;; esac; }
log() { printf '\n=== %s ===\n' "$*"; }

if [[ -n "$(git status --porcelain)" && "${ALLOW_DIRTY:-0}" != 1 ]]; then
  echo "The working tree has uncommitted changes; the images would not match any commit." >&2
  echo "Commit first, or ALLOW_DIRTY=1 to deploy the working tree anyway." >&2
  exit 1
fi
HEAD_SHA="$(git rev-parse --short=12 HEAD)"

# ---------- decide ----------
forced=0
for v in DEPLOY_FRONTEND DEPLOY_BACKEND RUN_MIGRATIONS APPLY_STACK DEPLOY_NGINX DEPLOY_TIMERS DEPLOY_KASPI; do
  [[ -n "${!v:-}" ]] && forced=1
done
if [[ "$FULL" == 1 ]]; then
  DEPLOY_FRONTEND=true DEPLOY_BACKEND=true RUN_MIGRATIONS=true APPLY_STACK=true DEPLOY_NGINX=true DEPLOY_TIMERS=true DEPLOY_KASPI=true
elif [[ "$forced" == 0 ]]; then
  BASE="${BASE:-}"
  if [[ -z "$BASE" && "${OFFLINE:-0}" != 1 ]]; then   # read-only, also in a dry run
    BASE="$(ssh "$VPS" "cat $APP_ROOT/docker/DEPLOYED_SHA 2>/dev/null" || true)"
  fi
  [[ -n "$BASE" ]] && git fetch -q origin 2>/dev/null || true
  decision="$(mktemp)"
  bash scripts/detect-smart-deploy.sh --base "${BASE:-}" --head HEAD --env-output "$decision"
  # shellcheck disable=SC1090
  . "$decision"; rm -f "$decision"
  if bool "${SKIP_DEPLOY:-false}"; then
    echo ""
    echo "Nothing to deploy (docs / tests / tooling only)."
    exit 0
  fi
fi
DEPLOY_FRONTEND="${DEPLOY_FRONTEND:-false}"; DEPLOY_BACKEND="${DEPLOY_BACKEND:-false}"
RUN_MIGRATIONS="${RUN_MIGRATIONS:-false}"; APPLY_STACK="${APPLY_STACK:-false}"
DEPLOY_NGINX="${DEPLOY_NGINX:-false}"; DEPLOY_TIMERS="${DEPLOY_TIMERS:-false}"; DEPLOY_KASPI="${DEPLOY_KASPI:-false}"
bool "$RUN_MIGRATIONS" && DEPLOY_BACKEND=true

log "Plan ($HEAD_SHA, mode $MODE)"
echo "web=$DEPLOY_FRONTEND api=$DEPLOY_BACKEND migrations=$RUN_MIGRATIONS stack=$APPLY_STACK nginx=$DEPLOY_NGINX timers=$DEPLOY_TIMERS kaspi=$DEPLOY_KASPI"
[[ "$DRY_RUN" == 1 ]] && { echo "(dry run: nothing built or sent)"; exit 0; }

# ---------- build here ----------
API_TAG="" WEB_TAG="" KASPI_TAG=""
if bool "$DEPLOY_BACKEND"; then
  API_TAG="$HEAD_SHA"
  log "Build studenthub-api:$API_TAG"
  DOCKER_BUILDKIT=1 docker build --platform "$PLATFORM" --build-arg BUILD_ID="$HEAD_SHA" \
    -f docker/backend.Dockerfile -t "studenthub-api:$API_TAG" backend/
fi
if bool "$DEPLOY_FRONTEND"; then
  WEB_TAG="$HEAD_SHA"
  log "Build studenthub-web:$WEB_TAG"
  DOCKER_BUILDKIT=1 docker build --platform "$PLATFORM" --build-arg BUILD_ID="$HEAD_SHA" \
    --build-arg NEXT_PUBLIC_SITE_URL="$SITE_URL" --build-arg API_ORIGIN=http://api:8110 \
    -f docker/frontend.Dockerfile -t "studenthub-web:$WEB_TAG" frontend/
fi

if bool "$DEPLOY_KASPI"; then
  KASPI_TAG="$HEAD_SHA"
  log "Build studenthub-kaspi-pos:$KASPI_TAG"
  DOCKER_BUILDKIT=1 docker build --platform "$PLATFORM" -t "studenthub-kaspi-pos:$KASPI_TAG" services/kaspi-pos/
fi

# ---------- ship ----------
ship() { # ship <image>: stream it over ssh, skip when the server already has it
  if ssh "$VPS" "docker image inspect $1 >/dev/null 2>&1"; then echo "$1 already on the server"; return; fi
  log "Ship $1 ($(docker image inspect -f '{{.Size}}' "$1" | awk '{printf "%.0f MB", $1/1e6}'))"
  docker save "$1" | gzip -1 | ssh "$VPS" 'gunzip | docker load'
}
[[ -n "$API_TAG" ]] && ship "studenthub-api:$API_TAG"
[[ -n "$WEB_TAG" ]] && ship "studenthub-web:$WEB_TAG"
[[ -n "$KASPI_TAG" ]] && ship "studenthub-kaspi-pos:$KASPI_TAG"

log "Copy compose, nginx and timer files"
ssh "$VPS" "mkdir -p $APP_ROOT/docker/systemd"
scp -q docker/docker-compose.yml docker/stack.yml docker/nginx.host.conf docker/nginx.conf docker/vps-apply.sh "$VPS:$APP_ROOT/docker/"
scp -q docker/systemd/* "$VPS:$APP_ROOT/docker/systemd/"

# ---------- apply there ----------
log "Apply on the server"
ssh "$VPS" "cd $APP_ROOT/docker && MODE=$MODE HEAD_SHA=$HEAD_SHA API_TAG=$API_TAG WEB_TAG=$WEB_TAG KASPI_TAG=$KASPI_TAG DEPLOY_KASPI=$DEPLOY_KASPI \
  DEPLOY_FRONTEND=$DEPLOY_FRONTEND DEPLOY_BACKEND=$DEPLOY_BACKEND RUN_MIGRATIONS=$RUN_MIGRATIONS \
  APPLY_STACK=$APPLY_STACK DEPLOY_NGINX=$DEPLOY_NGINX DEPLOY_TIMERS=$DEPLOY_TIMERS \
  SWITCH_FROM_SYSTEMD=${SWITCH_FROM_SYSTEMD:-0} bash vps-apply.sh"

log "Public check"
curl -fsS -m 15 "$SITE_URL/api/health" && echo " $SITE_URL/api/health ok" || echo "WARNING: $SITE_URL/api/health did not answer (DNS / certificate not switched yet?)"
