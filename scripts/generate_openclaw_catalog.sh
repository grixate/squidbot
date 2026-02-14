#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
ROOT_DIR="$(cd "${REPO_DIR}/.." && pwd)"

go run "${ROOT_DIR}/scripts/generate_openclaw_catalog.go" \
  -providers "${ROOT_DIR}/openclaw-parity/providers.json" \
  -channels "${ROOT_DIR}/openclaw-parity/channels.json" \
  -out "${REPO_DIR}/internal/catalog/openclaw_catalog.go" \
  -pkg "catalog"
