#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
set -a
source ./.env
set +a
export HELIOS_WORKSPACE_DIR=/tmp/helios-e2e/runs
export HELIOS_PUBLIC_API_BASE=http://localhost:8080
mkdir -p /tmp/helios-e2e
exec /tmp/helios-worker
