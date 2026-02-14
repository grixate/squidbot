#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

"${SCRIPT_DIR}/generate_openclaw_catalog.sh"

if ! git -C "${REPO_DIR}" diff --quiet -- internal/catalog/openclaw_catalog.go; then
  echo "openclaw catalog drift detected in squidbot."
  git -C "${REPO_DIR}" --no-pager diff -- internal/catalog/openclaw_catalog.go
  exit 1
fi

echo "squidbot openclaw catalog is in sync."
