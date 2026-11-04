#!/usr/bin/env bash
# Student Hub — the part of a deploy that runs ON the VPS. Never builds anything: the images were built elsewhere
# (docker/smart-deploy.sh on a laptop, or the GitHub workflow) and already arrived with `docker load`.
#
# Called over ssh with the decision in the environment, e.g.
#   ssh root@85.137.91.47 "DEPLOY_BACKEND=true API_TAG=<sha> HEAD_SHA=<sha> bash -s" < docker/vps-apply.sh
#
#   MODE              compose (default, the shared VPS) | swarm (docker/stack.yml, optional, see docs)
#   DEPLOY_FRONTEND   restart web with WEB_TAG          DEPLOY_BACKEND   restart api with API_TAG
#   RUN_MIGRATIONS    run /app/migrate (one-off container) before the new api starts
#   APPLY_STACK       re-apply the whole compose file / stack
#   DEPLOY_NGINX      install nginx.host.conf as /etc/nginx/conf.d/studenthub.conf (nginx -t, rollback on failure)
#   DEPLOY_TIMERS     install docker/systemd/*.service|*.timer and enable the timers
#   DEPLOY_KASPI      restart the café kaspi-pos sidecar with KASPI_TAG (only when ./.env has COMPOSE_PROFILES=cafe-kaspi)
#   SWITCH_FROM_SYSTEMD=1   one-time switch from the old systemd binary + docker-run Postgres (docs/deployment/vps.md)
#   HEAD_SHA          recorded in DEPLOYED_SHA after a healthy deploy (the next smart deploy diffs against it)
set -euo pipefail

APP_ROOT="${APP_ROOT:-/opt/studenthub}"
DOCKER_DIR="$APP_ROOT/docker"
APP_ENV="${STUDENTHUB_ENV_FILE:-$APP_ROOT/.env}"
SITE_HOST="${SITE_HOST:-studenthub.studywithraissov.com}"
NGINX_TARGET="${NGINX_TARGET:-/etc/nginx/conf.d/studenthub.conf}"
STACK_NAME="${STACK_NAME:-studenthub}"
MODE="${MODE:-compose}"

DEPLOY_FRONTEND="${DEPLOY_FRONTEND:-false}"
DEPLOY_BACKEND="${DEPLOY_BACKEND:-false}"
RUN_MIGRATIONS="${RUN_MIGRATIONS:-false}"
APPLY_STACK="${APPLY_STACK:-false}"
DEPLOY_NGINX="${DEPLOY_NGINX:-false}"
DEPLOY_TIMERS="${DEPLOY_TIMERS:-false}"
DEPLOY_KASPI="${DEPLOY_KASPI:-false}"
KASPI_TAG="${KASPI_TAG:-}"
SWITCH_FROM_SYSTEMD="${SWITCH_FROM_SYSTEMD:-0}"
API_TAG="${API_TAG:-}"
WEB_TAG="${WEB_TAG:-}"
HEAD_SHA="${HEAD_SHA:-}"

bool() { case "${1:-false}" in 1|true|TRUE|yes|on) return 0 ;; *) return 1 ;; esac; }
log() { printf '\n=== %s ===\n' "$*"; }
die() { echo "ERROR: $*" >&2; exit 1; }

cd "$DOCKER_DIR" || die "$DOCKER_DIR missing (smart-deploy.sh copies the files first)"
[[ -f "$APP_ENV" ]] || die "$APP_ENV missing: the application settings (DATABASE_URL, BOT_TOKEN, ...) live there"

# ---------- compose variables: ./.env next to docker-compose.yml ----------
touch .env && chmod 600 .env
set_kv() { # set_kv KEY VALUE: replace or append KEY=VALUE in ./.env
  local k="$1" v="$2"
  if grep -q "^${k}=" .env; then sed -i "s|^${k}=.*|${k}=${v}|" .env; else echo "${k}=${v}" >> .env; fi
}
get_kv() { grep -E "^$1=" .env | tail -n1 | cut -d= -f2- || true; }

PGPASS="$(grep -E '^DATABASE_URL=' "$APP_ENV" | sed -E 's#^DATABASE_URL=["'\'']?postgres(ql)?://[^:]+:([^@]+)@.*#\2#')"
[[ -n "$PGPASS" && "$PGPASS" != DATABASE_URL=* ]] || die "cannot read the Postgres password from DATABASE_URL in $APP_ENV"
set_kv POSTGRES_PASSWORD "$PGPASS"
if id studenthub >/dev/null 2>&1; then
  set_kv STUDENTHUB_UID "$(id -u studenthub)"
  set_kv STUDENTHUB_GID "$(id -g studenthub)"
fi
[[ -n "$API_TAG" ]] && set_kv API_TAG "$API_TAG"
[[ -n "$WEB_TAG" ]] && set_kv WEB_TAG "$WEB_TAG"
[[ -n "$KASPI_TAG" ]] && set_kv KASPI_TAG "$KASPI_TAG"
API_TAG="$(get_kv API_TAG)"; WEB_TAG="$(get_kv WEB_TAG)"
[[ -n "$API_TAG" && -n "$WEB_TAG" ]] || die "API_TAG / WEB_TAG unknown: run a full deploy first (bash docker/deploy.sh)"
docker image inspect "studenthub-api:$API_TAG" >/dev/null 2>&1 || die "image studenthub-api:$API_TAG not loaded"
docker image inspect "studenthub-web:$WEB_TAG" >/dev/null 2>&1 || die "image studenthub-web:$WEB_TAG not loaded"
mkdir -p "$APP_ROOT/uploads/market" "$APP_ROOT/uploads/quiz"
# café Kaspi sidecar: only where the operator switched its compose profile on (docs/cafe.md)
KASPI_ON=false
if get_kv COMPOSE_PROFILES | grep -q 'cafe-kaspi'; then
  KASPI_ON=true
  KASPI_TAG="$(get_kv KASPI_TAG)"
  [[ -n "$KASPI_TAG" ]] || die "COMPOSE_PROFILES has cafe-kaspi but KASPI_TAG is unknown: deploy with DEPLOY_KASPI=true first"
  docker image inspect "studenthub-kaspi-pos:$KASPI_TAG" >/dev/null 2>&1 || die "image studenthub-kaspi-pos:$KASPI_TAG not loaded"
  [[ -f "$APP_ROOT/kaspi-pos.env" || -n "$(get_kv KASPI_POS_ENV_FILE)" ]] || die "$APP_ROOT/kaspi-pos.env missing (docker/.env.example)"
fi
mkdir -p "$APP_ROOT/uploads/market" "$APP_ROOT/uploads/quiz" "$APP_ROOT/uploads/certs"

echo "mode=$MODE api=studenthub-api:$API_TAG web=studenthub-web:$WEB_TAG"
echo "deploy_frontend=$DEPLOY_FRONTEND deploy_backend=$DEPLOY_BACKEND run_migrations=$RUN_MIGRATIONS apply_stack=$APPLY_STACK nginx=$DEPLOY_NGINX timers=$DEPLOY_TIMERS"

# ---------- one-time switch from the old systemd setup ----------
old_api_running() { systemctl is-active --quiet studenthub-api 2>/dev/null; }
old_pg_unmanaged() { # the docker-run Postgres from deploy/deploy-vps.sh has no compose label
  docker inspect studenthub-postgres >/dev/null 2>&1 &&
    [[ -z "$(docker inspect -f '{{ index .Config.Labels "com.docker.compose.project" }}' studenthub-postgres 2>/dev/null)" ]]
}
if [[ "$MODE" == compose ]] && { old_api_running || old_pg_unmanaged; }; then
  [[ "$SWITCH_FROM_SYSTEMD" == 1 ]] || die "the old systemd API / docker-run Postgres is still running. Read docs/deployment/vps.md (switch-over) and rerun with SWITCH_FROM_SYSTEMD=1."
  log "Switch-over: backup, stop the old units, hand the volume to compose"
  mkdir -p "$APP_ROOT/backups"
  backup="$APP_ROOT/backups/pre-compose-$(date +%Y%m%d-%H%M%S).sql.gz"
  docker exec studenthub-postgres pg_dump -U studenthub -d studenthub | gzip > "$backup"
  [[ -s "$backup" ]] || die "backup $backup is empty, nothing was stopped"
  echo "backup: $backup ($(du -h "$backup" | cut -f1))"
  systemctl disable --now studenthub-api 2>/dev/null || true
  if old_pg_unmanaged; then
    docker stop studenthub-postgres >/dev/null
    docker rm studenthub-postgres >/dev/null   # the volume studenthub-pg stays; compose attaches to it
  fi
  APPLY_STACK=true
  DEPLOY_TIMERS=true
fi

# ---------- containers ----------
dc() { docker compose -f docker-compose.yml "$@"; }

wait_healthy() { # wait_healthy <container> [tries]
  local c="$1" n="${2:-60}" s
  for _ in $(seq 1 "$n"); do
    s="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$c" 2>/dev/null || echo missing)"
    [[ "$s" == healthy ]] && return 0
    sleep 2
  done
  echo "$c is $s" >&2
  docker logs --tail 60 "$c" >&2 || true
  return 1
}

if [[ "$MODE" == compose ]]; then
  docker volume inspect studenthub-pg >/dev/null 2>&1 || die "volume studenthub-pg missing: refusing to start an empty database (see docs/deployment/vps.md)"
  dc config -q
  log "Postgres"
  dc up -d postgres
  wait_healthy studenthub-postgres

  if bool "$RUN_MIGRATIONS"; then
    log "Migrations (studenthub-api:$API_TAG)"
    dc run --rm --no-deps api /app/migrate
  fi

  if bool "$APPLY_STACK"; then
    log "Apply the whole compose file"
    dc up -d --remove-orphans
  else
    if bool "$DEPLOY_BACKEND"; then log "api -> $API_TAG"; dc up -d --no-deps api; fi
    if bool "$DEPLOY_FRONTEND"; then log "web -> $WEB_TAG"; dc up -d --no-deps web; fi
    if bool "$DEPLOY_KASPI" && [[ "$KASPI_ON" == true ]]; then log "kaspi-pos -> $KASPI_TAG"; dc up -d --no-deps kaspi-pos; fi
  fi
  wait_healthy studenthub-api
  wait_healthy studenthub-web
  if [[ "$KASPI_ON" == true ]]; then wait_healthy studenthub-kaspi-pos; else echo "kaspi-pos: profile cafe-kaspi off, not running"; fi
elif [[ "$MODE" == swarm ]]; then
  docker info --format '{{.Swarm.LocalNodeState}}' | grep -q active || die "Swarm is not active: run docker/swarm-init.sh first (optional variant)"
  set -a; . ./.env; set +a
  export API_TAG WEB_TAG
  NGINX_CONFIG_VERSION="$(sha256sum nginx.conf | cut -c1-12)"; export NGINX_CONFIG_VERSION
  if bool "$RUN_MIGRATIONS"; then
    log "Migrations (swarm, one-off container on the stack network)"
    docker run --rm --network "${STACK_NAME}_internal" --env-file "$APP_ENV" \
      -e "DATABASE_URL=postgres://studenthub:${POSTGRES_PASSWORD}@postgres:5432/studenthub?sslmode=disable" \
      "studenthub-api:$API_TAG" /app/migrate
  fi
  if bool "$APPLY_STACK"; then
    log "docker stack deploy"
    docker stack deploy --resolve-image never -c stack.yml "$STACK_NAME"
  else
    bool "$DEPLOY_BACKEND" && docker service update --image "studenthub-api:$API_TAG" "${STACK_NAME}_api"
    bool "$DEPLOY_FRONTEND" && docker service update --image "studenthub-web:$WEB_TAG" "${STACK_NAME}_web"
  fi
else
  die "MODE must be compose or swarm"
fi

# ---------- host nginx ----------
if bool "$DEPLOY_NGINX" && [[ "$MODE" == compose ]]; then
  log "Host nginx"
  missing=""
  for h in "$SITE_HOST" studenthub.85-137-91-47.sslip.io; do
    [[ -f "/etc/letsencrypt/live/$h/fullchain.pem" ]] || missing="$missing $h"
  done
  if [[ -n "$missing" ]]; then
    echo "SKIPPED: no certificate for:$missing. Issue it first: certbot certonly --nginx -d <host>; then rerun with DEPLOY_NGINX=true" >&2
  elif cmp -s nginx.host.conf "$NGINX_TARGET"; then
    echo "nginx: unchanged"
  else
    [[ -f "$NGINX_TARGET" ]] && cp -p "$NGINX_TARGET" "$NGINX_TARGET.bak-$(date +%Y%m%d-%H%M%S)"
    prev="$(mktemp)"; [[ -f "$NGINX_TARGET" ]] && cp -p "$NGINX_TARGET" "$prev"
    cp nginx.host.conf "$NGINX_TARGET"
    if nginx -t; then
      systemctl reload nginx
      echo "nginx: installed and reloaded"
    else
      if [[ -s "$prev" ]]; then cp "$prev" "$NGINX_TARGET"; else rm -f "$NGINX_TARGET"; fi
      nginx -t && systemctl reload nginx || true
      die "nginx -t failed, previous config restored"
    fi
    rm -f "$prev"
  fi
fi

# ---------- timers (host systemd; they curl the API on 127.0.0.1:8110 with CRON_SECRET) ----------
if bool "$DEPLOY_TIMERS"; then
  log "Timers"
  cp systemd/studenthub-cron.service systemd/studenthub-cron.timer systemd/studenthub-notify.service systemd/studenthub-notify.timer /etc/systemd/system/
  systemctl daemon-reload
  systemctl enable --now studenthub-cron.timer studenthub-notify.timer
  systemctl list-timers 'studenthub-*' --no-pager || true
fi

# ---------- health ----------
log "Health"
ok=1
for _ in $(seq 1 20); do curl -fsS -m 5 http://127.0.0.1:8110/api/health >/dev/null && break; sleep 2; done
curl -fsS -m 5 http://127.0.0.1:8110/api/health >/dev/null && echo "api   127.0.0.1:8110 ok" || { echo "api   127.0.0.1:8110 FAILED"; ok=0; }
curl -fsS -m 5 -o /dev/null http://127.0.0.1:8112/ && echo "web   127.0.0.1:8112 ok" || { echo "web   127.0.0.1:8112 FAILED"; ok=0; }
if [[ -f "/etc/letsencrypt/live/$SITE_HOST/fullchain.pem" ]] && grep -q "$SITE_HOST" "$NGINX_TARGET" 2>/dev/null; then
  for _ in $(seq 1 10); do curl -fsS -m 10 "https://$SITE_HOST/api/health" >/dev/null && break; sleep 3; done
  curl -fsS -m 10 "https://$SITE_HOST/api/health" >/dev/null && echo "https://$SITE_HOST/api/health ok" || { echo "https://$SITE_HOST/api/health FAILED"; ok=0; }
  curl -fsS -m 10 -o /dev/null "https://$SITE_HOST/" && echo "https://$SITE_HOST/ ok" || { echo "https://$SITE_HOST/ FAILED"; ok=0; }
fi
[[ "$ok" == 1 ]] || die "health checks failed (previous tags: see docs/deployment/vps.md, rollback)"

[[ -n "$HEAD_SHA" ]] && echo "$HEAD_SHA" > DEPLOYED_SHA
date -Is > DEPLOYED_AT

# keep the 3 newest tags of each image (rollback material), drop older ones and dangling layers
for repo in studenthub-api studenthub-web; do
  docker images "$repo" --format '{{.CreatedAt}}\t{{.Repository}}:{{.Tag}}' | sort -r | tail -n +4 | cut -f2 |
    while read -r img; do
      [[ "$img" == "studenthub-api:$API_TAG" || "$img" == "studenthub-web:$WEB_TAG" ]] && continue
      docker rmi "$img" >/dev/null 2>&1 || true
    done
done
docker image prune -f >/dev/null 2>&1 || true

log "Done: api=$API_TAG web=$WEB_TAG"
