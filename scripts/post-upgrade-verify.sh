#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

echo "==> Checking Docker CLI"
docker version
docker compose version

echo "==> Building and starting AI Gateway"
docker compose up --build -d

echo "==> Waiting for health check"
for _ in $(seq 1 60); do
  if curl -fsS http://localhost:8080/healthz >/dev/null; then
    echo "AI Gateway is healthy."
    echo "Dashboard: http://localhost:8080"
    echo "Swagger UI: http://localhost:8080/docs"
    echo "OpenAPI:   http://localhost:8080/openapi.yaml"
    exit 0
  fi
  sleep 2
done

echo "AI Gateway did not become healthy in time. Recent logs:"
docker compose logs --tail=100
exit 1
