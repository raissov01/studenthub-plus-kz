#!/bin/sh
# Idempotently adds the quiz locations to an existing Student Hub nginx server file:
#   - /api/quiz/live/ws: WebSocket upgrade (HTTP/1.1, Upgrade/Connection headers, 1 h timeouts)
#   - /api/quiz/generate/: lecture-note uploads (up to 5 × 10 MB) and slow AI calls
# deploy-vps.sh writes studenthub.conf only when it is absent (certbot edits it afterwards),
# so servers set up before the quiz module need this patch. Each block is inserted before every
# "location /api/ {" line, only if that block is not in the file yet. After a change nginx -t
# must pass, otherwise the previous file is restored.
#   ssh root@vps 'sh -s /etc/nginx/conf.d/studenthub.conf' < deploy/nginx-quiz-patch.sh
# SKIP_NGINX_TEST=1 skips the nginx -t check (local tests on a copy).
set -eu
f=${1:?usage: nginx-quiz-patch.sh /etc/nginx/conf.d/studenthub.conf}
[ -f "$f" ] || { echo "no $f" >&2; exit 1; }
has_ws=$(grep -c 'location = /api/quiz/live/ws' "$f" || true)
has_gen=$(grep -c 'location \^~ /api/quiz/generate/' "$f" || true)
if [ "$has_ws" -gt 0 ] && [ "$has_gen" -gt 0 ]; then
  echo "nginx: quiz locations present"
  exit 0
fi
tmp=$(mktemp)
awk -v ws="$has_ws" -v gen="$has_gen" '
/^[ \t]*location \/api\/ \{/ {
  match($0, /^[ \t]*/); i = substr($0, 1, RLENGTH)
  if (ws == 0) {
    print i "location = /api/quiz/live/ws {"
    print i "    proxy_pass http://127.0.0.1:8110;"
    print i "    proxy_http_version 1.1;"
    print i "    proxy_set_header Upgrade $http_upgrade;"
    print i "    proxy_set_header Connection \"upgrade\";"
    print i "    proxy_set_header Host $host;"
    print i "    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;"
    print i "    proxy_read_timeout 3600s;"
    print i "    proxy_send_timeout 3600s;"
    print i "    proxy_buffering off;"
    print i "}"
  }
  if (gen == 0) {
    print i "location ^~ /api/quiz/generate/ {"
    print i "    client_max_body_size 52m;"
    print i "    proxy_pass http://127.0.0.1:8110;"
    print i "    proxy_set_header Host $host;"
    print i "    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;"
    print i "    proxy_read_timeout 180s;"
    print i "}"
  }
}
{ print }' "$f" > "$tmp"
if cmp -s "$f" "$tmp"; then
  rm -f "$tmp"
  echo "nginx: no \"location /api/ {\" found in $f, nothing changed" >&2
  exit 1
fi
cp -p "$f" "$f.bak-quiz"
cat "$tmp" > "$f"
rm -f "$tmp"
if [ "${SKIP_NGINX_TEST:-}" != "1" ] && ! nginx -t >/dev/null 2>&1; then
  cat "$f.bak-quiz" > "$f"
  echo "nginx: -t failed after the quiz patch, previous config restored" >&2
  exit 1
fi
echo "nginx: quiz locations added"
