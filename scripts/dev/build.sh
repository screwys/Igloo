#!/usr/bin/env sh
# Build Go binary, optionally restart server and/or build Android.
# Usage:
#   build.sh              — build Go only
#   build.sh restart      — build Go + restart server
#   build.sh android      — build Go + build/install Android APK
#   build.sh all          — build Go + restart + Android
#   build.sh full         — build Go + daemon-reload + restart
set -eu

path_prepend_if_dir() {
  if [ -d "$1" ]; then
    case ":$PATH:" in
      *":$1:"*) ;;
      *) PATH="$1:$PATH" ;;
    esac
  fi
}

BREW_PREFIX="${HOMEBREW_PREFIX:-}"
if [ -z "$BREW_PREFIX" ] && command -v brew >/dev/null 2>&1; then
  BREW_PREFIX="$(brew --prefix 2>/dev/null || true)"
fi

path_prepend_if_dir "$HOME/.deno/bin"
if [ -n "$BREW_PREFIX" ]; then
  path_prepend_if_dir "$BREW_PREFIX/sbin"
  path_prepend_if_dir "$BREW_PREFIX/bin"
fi
path_prepend_if_dir /home/linuxbrew/.linuxbrew/sbin
path_prepend_if_dir /home/linuxbrew/.linuxbrew/bin
path_prepend_if_dir /opt/homebrew/sbin
path_prepend_if_dir /opt/homebrew/bin
path_prepend_if_dir "$HOME/go/bin"
path_prepend_if_dir "$HOME/.local/bin"
export PATH

cd "$(dirname "$0")/../.."

# ── Templ ──
echo "[templ] generating..."
templ generate
echo "[templ] ok"

# ── JS ──
echo "[esbuild] bundling..."
go run ./cmd/igloo-assets
echo "[esbuild] ok"

# ── Go ──
echo "[go] building..."
go build -o bin/igloo ./cmd/igloo/
go build -o bin/igloo-mcp ./cmd/igloo-mcp/
echo "[go] ok"

case "${1:-}" in
  mcp)
    echo "[mcp] building..."
    go build -o bin/igloo-mcp ./cmd/igloo-mcp/
    echo "[mcp] ok"
    exit 0
    ;;
  restart)
    scripts/dev/restart_igloo.sh
    ;;
  android)
    echo "[android] building..."
    android/build.sh
    ;;
  all)
    scripts/dev/restart_igloo.sh
    echo "[android] building..."
    android/build.sh
    ;;
  full)
    scripts/dev/restart_igloo_full.sh
    ;;
  "")
    ;;
  *)
    echo "usage: build.sh [restart|android|all|full]"
    exit 1
    ;;
esac
