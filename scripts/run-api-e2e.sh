#!/usr/bin/env bash
# 本地 e2e 用启动脚本: 加载 .env, 跑 api。
set -euo pipefail
cd "$(dirname "$0")/.."
set -a
source ./.env
set +a
export HELIOS_WORKSPACE_DIR=/tmp/helios-e2e/runs
export HELIOS_PUBLIC_API_BASE=http://localhost:8080
mkdir -p /tmp/helios-e2e
exec /tmp/helios-api
