#!/bin/bash
# Deploy the Student Hub Go API + its Postgres to the shared balu-fit VPS. Nothing is built on the
# server (its RAM is shared with client bots): the binaries are built here and copied. Re-runnable.
#   bash deploy/deploy-vps.sh
set -euo pipefail
cd "$(dirname "$0")/.."
VPS=root@85.137.91.47
HOST=studenthub.85-137-91-47.sslip.io

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o deploy/studenthub-api ./cmd/server
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o deploy/studenthub-migrate ./cmd/migrate

ssh $VPS "id studenthub >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin studenthub; mkdir -p /opt/studenthub"
ssh $VPS "mkdir -p /opt/studenthub/uploads/market && chown -R studenthub:studenthub /opt/studenthub/uploads && chmod 750 /opt/studenthub/uploads /opt/studenthub/uploads/market"
ssh $VPS "test -f /opt/studenthub/.env" || scp -q .secrets/prod.env $VPS:/opt/studenthub/.env
ssh $VPS "chown root:studenthub /opt/studenthub/.env && chmod 640 /opt/studenthub/.env"

# Postgres: its own small container, localhost only, memory capped
PGPASS=$(grep ^DATABASE_URL .secrets/prod.env | sed -E 's#.*://studenthub:([^@]+)@.*#\1#')
ssh $VPS "docker inspect studenthub-postgres >/dev/null 2>&1 || docker run -d --name studenthub-postgres --restart unless-stopped \
  -e POSTGRES_USER=studenthub -e POSTGRES_PASSWORD=$PGPASS -e POSTGRES_DB=studenthub \
  -p 127.0.0.1:5442:5432 -v studenthub-pg:/var/lib/postgresql/data --memory 160m \
  postgres:16-alpine -c shared_buffers=24MB -c max_connections=20"
ssh $VPS 'for i in $(seq 1 30); do docker exec studenthub-postgres pg_isready -U studenthub -q && exit 0; sleep 1; done; exit 1'

rsync -az --delete data/ $VPS:/opt/studenthub/data/
ssh $VPS "mkdir -p /opt/studenthub/supabase/migrations"
rsync -az --delete supabase/migrations/ $VPS:/opt/studenthub/supabase/migrations/
scp -q deploy/studenthub-api $VPS:/opt/studenthub/studenthub-api.new
scp -q deploy/studenthub-migrate $VPS:/opt/studenthub/studenthub-migrate
ssh $VPS "mv /opt/studenthub/studenthub-api.new /opt/studenthub/studenthub-api && chmod 755 /opt/studenthub/studenthub-api /opt/studenthub/studenthub-migrate"
ssh $VPS "cd /opt/studenthub && set -a && . ./.env && set +a && ./studenthub-migrate"

sed "s/__HOST__/$HOST/" deploy/studenthub.nginx.conf | ssh $VPS "test -f /etc/nginx/conf.d/studenthub.conf || cat > /etc/nginx/conf.d/studenthub.conf"
scp -q deploy/studenthub-api.service deploy/studenthub-cron.service deploy/studenthub-cron.timer deploy/studenthub-notify.service deploy/studenthub-notify.timer $VPS:/etc/systemd/system/
ssh $VPS "systemctl daemon-reload && systemctl enable --now studenthub-api studenthub-cron.timer studenthub-notify.timer && systemctl restart studenthub-api && nginx -t && systemctl reload nginx"
ssh $VPS "grep -q 'listen 443' /etc/nginx/conf.d/studenthub.conf || certbot --nginx -d $HOST --non-interactive --agree-tos --register-unsafely-without-email --redirect >/dev/null"
ssh $VPS 'for i in $(seq 1 20); do curl -sf localhost:8110/api/health >/dev/null && echo "api up" && exit 0; sleep 1; done; journalctl -u studenthub-api -n 30 --no-pager; exit 1'
curl -sf "https://$HOST/api/health" && echo " https ok"
