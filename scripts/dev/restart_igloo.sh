#!/usr/bin/env sh
set -eu

echo "[igloo] stopping service..."
systemctl --user stop igloo.service 2>/dev/null || true

# Kill anything still holding port 5001
PID=$(ss -tlnp | grep ':5001 ' | sed -n 's/.*pid=\([0-9]*\).*/\1/p')
if [ -n "$PID" ]; then
  echo "[igloo] killing leftover pid $PID on :5001"
  kill "$PID" 2>/dev/null || true
  sleep 1
fi

echo "[igloo] starting service..."
systemctl --user start igloo.service

HEALTH_URL="${IGLOO_HEALTH_URL:-https://127.0.0.1:8443/api/health/live}"
TIMEOUT_SECONDS="${IGLOO_RESTART_TIMEOUT_SECONDS:-120}"

echo "[igloo] waiting for health: $HEALTH_URL"
elapsed=0
while [ "$elapsed" -lt "$TIMEOUT_SECONDS" ]; do
  if curl -k -fsS -o /dev/null "$HEALTH_URL" 2>/dev/null; then
    echo "[igloo] running"
    exit 0
  fi
  if systemctl --user is-failed --quiet igloo.service; then
    echo "[igloo] FAILED"
    systemctl --user status igloo.service --no-pager || true
    journalctl --user -u igloo.service -n 80 --no-pager || true
    exit 1
  fi
  sleep 1
  elapsed=$((elapsed + 1))
done

echo "[igloo] FAILED: health did not return 200 within ${TIMEOUT_SECONDS}s"
systemctl --user status igloo.service --no-pager || true
journalctl --user -u igloo.service -n 80 --no-pager || true
exit 1
