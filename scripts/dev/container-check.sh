#!/usr/bin/env bash
set -euo pipefail

runtime="${CONTAINER_RUNTIME:-}"
if [[ -z "$runtime" ]]; then
  if command -v docker >/dev/null 2>&1; then
    runtime=docker
  elif command -v podman >/dev/null 2>&1; then
    runtime=podman
  else
    echo "docker or podman is required" >&2
    exit 1
  fi
fi

image="${IGLOO_CONTAINER_CHECK_IMAGE:-ghcr.io/screwys/igloo:container-check}"
port="${IGLOO_CONTAINER_CHECK_PORT:-5011}"
name="igloo-container-check-$$"
tmp="$(mktemp -d)"
state_volume="${name}-state"
build_image="${IGLOO_CONTAINER_CHECK_BUILD:-1}"

cleanup() {
  "$runtime" rm -f "$name" >/dev/null 2>&1 || true
  "$runtime" volume rm -f "$state_volume" >/dev/null 2>&1 || true
  rm -rf "$tmp"
}
trap cleanup EXIT

if [[ "$build_image" != "0" ]]; then
  "$runtime" build -t "$image" .
fi

"$runtime" volume create "$state_volume" >/dev/null

"$runtime" run --rm \
  -e IGLOO_ENABLED_PLATFORMS=all \
  -v "$state_volume:/igloo" \
  "$image" \
  /usr/local/bin/igloo-adduser -username check -password check-pass -platforms youtube >/dev/null

"$runtime" run -d --name "$name" \
  -e IGLOO_ENABLED_PLATFORMS=all \
  -v "$state_volume:/igloo" \
  -p "127.0.0.1:${port}:5001" \
  "$image" >/dev/null

for _ in $(seq 1 60); do
  if curl -fsS "http://127.0.0.1:${port}/api/health/live" >/dev/null; then
    break
  fi
  sleep 1
done

curl -fsS "http://127.0.0.1:${port}/api/health/live" >/dev/null
curl -fsS "http://127.0.0.1:${port}/static/style.css" >/dev/null
login_html="$(curl -fsS -c "$tmp/igloo-check-cookies.txt" "http://127.0.0.1:${port}/login")"
csrf="$(printf '%s\n' "$login_html" | sed -n 's/.*name="_csrf_token" value="\([^"]*\)".*/\1/p' | head -n1)"
if [[ -z "$csrf" ]]; then
 echo "login page did not include CSRF token" >&2
 exit 1
fi
status="$(curl -fsS -b "$tmp/igloo-check-cookies.txt" -c "$tmp/igloo-check-cookies.txt" \
  --data-urlencode "_csrf_token=$csrf" \
  --data-urlencode "username=check" \
  --data-urlencode "password=check-pass" \
  -o /dev/null -w '%{http_code}' \
  "http://127.0.0.1:${port}/login")"
if [[ "$status" != "303" ]]; then
  echo "login POST returned HTTP $status, want 303" >&2
  exit 1
fi

echo "container check ok on http://127.0.0.1:${port}"
