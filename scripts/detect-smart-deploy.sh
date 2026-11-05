#!/usr/bin/env bash
# Student Hub — decide what a deploy has to touch, from the files changed between two commits.
#
#   bash scripts/detect-smart-deploy.sh --base <deployed sha> --head HEAD [--github-output "$GITHUB_OUTPUT"] [--env-output file]
#
# Buckets (docs/deployment/smart-deploy.md):
#   frontend   -> rebuild + restart web          backend    -> rebuild + restart api
#   migrations -> run /app/migrate (and api)     stack      -> re-apply docker-compose.yml / stack.yml
#   nginx      -> install nginx.host.conf        timers     -> install docker/systemd units
#   kaspi      -> rebuild + restart the café kaspi-pos sidecar (services/kaspi-pos; only where its profile is on)
#   docs / tests / tooling only -> skip the deploy entirely
#   anything unknown -> safe fallback: full deploy (web + api + stack + nginx + timers)
set -euo pipefail

BASE_REF=""
HEAD_REF="HEAD"
GITHUB_OUTPUT_FILE=""
ENV_OUTPUT_FILE=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --base) BASE_REF="${2:-}"; shift 2 ;;
    --head) HEAD_REF="${2:-}"; shift 2 ;;
    --github-output) GITHUB_OUTPUT_FILE="${2:-}"; shift 2 ;;
    --env-output) ENV_OUTPUT_FILE="${2:-}"; shift 2 ;;
    *) echo "Unknown argument: $1" >&2; exit 1 ;;
  esac
done

# No usable base (first deploy, unknown sha, all-zero sha from a new branch): compare with an empty tree, which
# lists every file and therefore deploys everything.
if [[ -z "$BASE_REF" || "$BASE_REF" =~ ^0+$ ]] || ! git rev-parse --verify -q "${BASE_REF}^{commit}" >/dev/null; then
  BASE_REF="$(git hash-object -t tree /dev/null)"
fi

mapfile -t CHANGED_FILES < <(git diff --name-only "$BASE_REF" "$HEAD_REF" | sed '/^$/d')

FRONTEND_FILES=()
BACKEND_FILES=()
MIGRATION_FILES=()
STACK_FILES=()
NGINX_FILES=()
TIMER_FILES=()
KASPI_FILES=()
DOC_FILES=()
TOOLING_FILES=()
UNKNOWN_FILES=()

for file in "${CHANGED_FILES[@]}"; do
  case "$file" in
    # docs and notes
    docs/*|README.md|AGENTS.md|CLAUDE.md|*/README.md|frontend/AGENTS.md|archive/*)
      DOC_FILES+=("$file") ;;
    # tests and developer tooling: never part of a running image's behaviour
    frontend/e2e/*|frontend/tools/*|frontend/playwright.config.ts|frontend/playwright.real.config.ts|*.test.ts|\
    backend/*_test.go|backend/internal/testdb/*|backend/cmd/e2etest/*|backend/cmd/routes/*|backend/cmd/json-to-csv/*|\
    backend/templates/*|backend/sqlc.yaml|backend/cmd/cafe-seed/*|\
    services/kaspi-pos/test/*|services/kaspi-pos/docs/*|services/kaspi-pos/*.md|services/kaspi-pos/UPSTREAM|\
    services/kaspi-pos/.env.example|services/kaspi-pos/webhooks*.example.json|services/kaspi-pos/.prettierrc|\
    services/kaspi-pos/.editorconfig|services/kaspi-pos/eslint.config.js|services/kaspi-pos/.gitignore|\
    .github/*|.claude/*|scripts/*|.gitignore|frontend/.gitignore|backend/.gitignore|\
    docker/*.sh|docker/docker-compose.local.yml|docker/docker-compose.override.yml|docker/nginx.local.conf|docker/*.example|docker/local.env.example)
      TOOLING_FILES+=("$file") ;;
    frontend/*|docker/frontend.Dockerfile)
      FRONTEND_FILES+=("$file") ;;
    backend/*|docker/backend.Dockerfile)
      BACKEND_FILES+=("$file") ;;
    docker/docker-compose.yml|docker/stack.yml|docker/nginx.conf)
      STACK_FILES+=("$file") ;;
    docker/nginx.host.conf)
      NGINX_FILES+=("$file") ;;
    docker/systemd/*)
      TIMER_FILES+=("$file") ;;
    services/kaspi-pos/*)
      KASPI_FILES+=("$file") ;;
    *)
      UNKNOWN_FILES+=("$file") ;;
  esac

  case "$file" in
    backend/migrations/*|backend/cmd/migrate/*)
      MIGRATION_FILES+=("$file") ;;
  esac
done

DEPLOY_FRONTEND=false
DEPLOY_BACKEND=false
RUN_MIGRATIONS=false
APPLY_STACK=false
DEPLOY_NGINX=false
DEPLOY_TIMERS=false
DEPLOY_KASPI=false
SKIP_DEPLOY=false
SAFE_FALLBACK_FULL_DEPLOY=false

[[ ${#FRONTEND_FILES[@]} -gt 0 ]] && DEPLOY_FRONTEND=true
[[ ${#BACKEND_FILES[@]} -gt 0 ]] && DEPLOY_BACKEND=true
if [[ ${#MIGRATION_FILES[@]} -gt 0 ]]; then
  RUN_MIGRATIONS=true
  DEPLOY_BACKEND=true
fi
[[ ${#STACK_FILES[@]} -gt 0 ]] && APPLY_STACK=true
[[ ${#NGINX_FILES[@]} -gt 0 ]] && DEPLOY_NGINX=true
[[ ${#TIMER_FILES[@]} -gt 0 ]] && DEPLOY_TIMERS=true
[[ ${#KASPI_FILES[@]} -gt 0 ]] && DEPLOY_KASPI=true

if [[ ${#UNKNOWN_FILES[@]} -gt 0 ]]; then
  SAFE_FALLBACK_FULL_DEPLOY=true
  DEPLOY_FRONTEND=true
  DEPLOY_BACKEND=true
  APPLY_STACK=true
  DEPLOY_NGINX=true
  DEPLOY_TIMERS=true
  DEPLOY_KASPI=true
fi

if [[ "$DEPLOY_FRONTEND" == false && "$DEPLOY_BACKEND" == false && "$APPLY_STACK" == false \
      && "$DEPLOY_NGINX" == false && "$DEPLOY_TIMERS" == false && "$DEPLOY_KASPI" == false ]]; then
  SKIP_DEPLOY=true
fi

join_lines() {
  local -n arr_ref="$1"
  [[ ${#arr_ref[@]} -eq 0 ]] && return
  printf '%s\n' "${arr_ref[@]}"
}

to_base64() { base64 | tr -d '\n'; }

SUMMARY="$(cat <<SUM
Smart deploy analysis
- Base ref: $BASE_REF
- Head ref: $HEAD_REF
- Changed files: ${#CHANGED_FILES[@]}
- Frontend (web) deploy: $DEPLOY_FRONTEND
- Backend (api) deploy: $DEPLOY_BACKEND
- Run migrations: $RUN_MIGRATIONS
- Stack apply: $APPLY_STACK
- Host nginx: $DEPLOY_NGINX
- Timers: $DEPLOY_TIMERS
- Café kaspi-pos sidecar: $DEPLOY_KASPI
- Skip deploy: $SKIP_DEPLOY
- Safe fallback full deploy: $SAFE_FALLBACK_FULL_DEPLOY
SUM
)"

echo "$SUMMARY"

print_bucket() {
  local title="$1" name="$2"
  local -n ref="$name"
  if [[ ${#ref[@]} -gt 0 ]]; then
    echo ""
    echo "$title:"
    join_lines "$name" | sed 's/^/- /'
  fi
}
print_bucket "Frontend-affecting files" FRONTEND_FILES
print_bucket "Backend-affecting files" BACKEND_FILES
print_bucket "Migration-affecting files" MIGRATION_FILES
print_bucket "Stack runtime files" STACK_FILES
print_bucket "Host nginx files" NGINX_FILES
print_bucket "Timer units" TIMER_FILES
print_bucket "Café kaspi-pos sidecar" KASPI_FILES
print_bucket "Docs-only files" DOC_FILES
print_bucket "Tests / tooling (no deploy)" TOOLING_FILES
print_bucket "Unknown files (safe fallback = full deploy)" UNKNOWN_FILES

if [[ -n "$GITHUB_OUTPUT_FILE" ]]; then
  {
    echo "skip_deploy=$SKIP_DEPLOY"
    echo "deploy_frontend=$DEPLOY_FRONTEND"
    echo "deploy_backend=$DEPLOY_BACKEND"
    echo "run_migrations=$RUN_MIGRATIONS"
    echo "apply_stack=$APPLY_STACK"
    echo "deploy_nginx=$DEPLOY_NGINX"
    echo "deploy_timers=$DEPLOY_TIMERS"
    echo "deploy_kaspi=$DEPLOY_KASPI"
    echo "safe_fallback_full_deploy=$SAFE_FALLBACK_FULL_DEPLOY"
    echo "changed_files_b64=$(join_lines CHANGED_FILES | to_base64)"
    echo "summary_b64=$(printf '%s\n' "$SUMMARY" | to_base64)"
  } >> "$GITHUB_OUTPUT_FILE"
fi

if [[ -n "$ENV_OUTPUT_FILE" ]]; then
  {
    echo "SKIP_DEPLOY=$SKIP_DEPLOY"
    echo "DEPLOY_FRONTEND=$DEPLOY_FRONTEND"
    echo "DEPLOY_BACKEND=$DEPLOY_BACKEND"
    echo "RUN_MIGRATIONS=$RUN_MIGRATIONS"
    echo "APPLY_STACK=$APPLY_STACK"
    echo "DEPLOY_NGINX=$DEPLOY_NGINX"
    echo "DEPLOY_TIMERS=$DEPLOY_TIMERS"
    echo "DEPLOY_KASPI=$DEPLOY_KASPI"
  } > "$ENV_OUTPUT_FILE"
fi
