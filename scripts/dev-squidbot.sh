#!/usr/bin/env bash
set -euo pipefail

# Runs squidbot with an isolated HOME so each clone has independent state.
# Default state root: <repo>/.dev-home

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
DEV_HOME_DEFAULT="${REPO_ROOT}/.dev-home"
DEV_HOME="${SQUIDBOT_DEV_HOME:-$DEV_HOME_DEFAULT}"
BIN_DIR="${REPO_ROOT}/.dev-bin"
LOCAL_BIN="${BIN_DIR}/squidbot"
HOST_HOME="${HOME:-}"

show_usage() {
  cat <<'USAGE'
Usage:
  scripts/dev-squidbot.sh <squidbot args...>
  scripts/dev-squidbot.sh reset
  scripts/dev-squidbot.sh where

Environment:
  SQUIDBOT_DEV_HOME   Override the isolated HOME directory path.

Examples:
  scripts/dev-squidbot.sh onboard
  scripts/dev-squidbot.sh gateway --with-manage
  scripts/dev-squidbot.sh status
  scripts/dev-squidbot.sh reset
USAGE
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  show_usage
  exit 0
fi

if [[ "${1:-}" == "where" ]]; then
  echo "DEV_HOME=${DEV_HOME}"
  echo "CONFIG=${DEV_HOME}/.squidbot/config.json"
  echo "DATA=${DEV_HOME}/.squidbot/data"
  echo "WORKSPACE=${DEV_HOME}/.squidbot/workspace"
  exit 0
fi

if [[ "${1:-}" == "reset" ]]; then
  rm -rf "${DEV_HOME}/.squidbot"
  echo "Removed ${DEV_HOME}/.squidbot"
  exit 0
fi

if [[ $# -eq 0 ]]; then
  show_usage
  exit 1
fi

mkdir -p "${DEV_HOME}"
cd "${REPO_ROOT}"

if [[ -x "${REPO_ROOT}/squidbot" ]]; then
  HOME="${DEV_HOME}" "${REPO_ROOT}/squidbot" "$@"
else
  mkdir -p "${BIN_DIR}"
  HOME="${HOST_HOME}" go build -o "${LOCAL_BIN}" ./cmd/squidbot
  HOME="${DEV_HOME}" "${LOCAL_BIN}" "$@"
fi
