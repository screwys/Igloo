#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

status=0
while IFS= read -r -d '' workflow; do
  while IFS= read -r line; do
    if [[ ! "$line" =~ ^[[:space:]]*uses:[[:space:]]+([^#[:space:]]+)@([^#[:space:]]+) ]]; then
      continue
    fi
    ref="${BASH_REMATCH[2]}"
    if [[ ! "$ref" =~ ^[0-9a-f]{40}$ ]]; then
      echo "${workflow#"$ROOT/"}: mutable action reference: ${BASH_REMATCH[1]}@$ref" >&2
      status=1
    fi
  done < "$workflow"
done < <(find .github/workflows -type f \( -name '*.yml' -o -name '*.yaml' \) -print0)

if [[ "$status" -eq 0 ]]; then
  echo "[workflows] action references are SHA-pinned"
fi

exit "$status"
