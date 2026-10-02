#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"
. scripts/dev/go-tool-versions.sh

tool_dir="$repo_root/.local/tools/golangci-lint/$GOLANGCI_LINT_VERSION"
lint_binary="$tool_dir/golangci-lint"
if [[ "$(go env GOHOSTOS)" == windows ]]; then
  lint_binary+=.exe
fi
if [[ ! -x "$lint_binary" ]]; then
  installer="$(mktemp)"
  trap 'rm -f "$installer"' EXIT
  curl -fsSL "https://raw.githubusercontent.com/golangci/golangci-lint/$GOLANGCI_LINT_VERSION/install.sh" -o "$installer"
  sh "$installer" -b "$tool_dir" "$GOLANGCI_LINT_VERSION"
  rm -f "$installer"
  trap - EXIT
fi

export GOLANGCI_LINT_CACHE="${GOLANGCI_LINT_CACHE:-$repo_root/.local/golangci-lint-cache}"
if (( $# == 0 )); then
  set -- run
fi
exec "$lint_binary" "$@"
