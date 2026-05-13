#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

go test ./internal/db -run 'SchemaSnapshot|SchemaTableLifecycles|SchemaMigrationLedger' "$@" -count=1
