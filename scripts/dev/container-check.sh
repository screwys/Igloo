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

runtime_is_podman() {
  local candidate="$1"

  [[ "$(basename "$candidate")" == "podman" ]] \
    || "$candidate" --version 2>/dev/null | grep -qi podman
}

repo_root="$(git rev-parse --show-toplevel)"
image="${IGLOO_CONTAINER_CHECK_IMAGE:-ghcr.io/screwys/igloo:container-check}"
port="${IGLOO_CONTAINER_CHECK_PORT:-5011}"
name="igloo-container-check-$$"
tmp="$(mktemp -d)"
state_volume="${name}-state"
adduser_volume="${name}-adduser-state"
build_image="${IGLOO_CONTAINER_CHECK_BUILD:-1}"
base_url="http://localhost:${port}"
curl_local=(--resolve "localhost:${port}:127.0.0.1")

compose() {
  IGLOO_ENABLED_PLATFORMS=all \
    IGLOO_IMAGE="$image" \
    IGLOO_PORT="$port" \
    IGLOO_STATE_VOLUME="$state_volume" \
    "$runtime" compose --project-name "$name" --file "$repo_root/compose.yaml" "$@"
}

cleanup() {
  compose down --volumes --remove-orphans >/dev/null 2>&1 || true
  "$runtime" volume rm -f "$state_volume" >/dev/null 2>&1 || true
  "$runtime" volume rm -f "$adduser_volume" >/dev/null 2>&1 || true
  rm -rf "$tmp"
}
trap cleanup EXIT

if ! "$runtime" compose version >/dev/null 2>&1; then
  echo "$runtime compose is required" >&2
  exit 1
fi

if [[ "$build_image" != "0" ]]; then
  "$runtime" build -t "$image" "$repo_root"
fi

"$runtime" volume create "$adduser_volume" >/dev/null

"$runtime" run --rm \
  -e IGLOO_ENABLED_PLATFORMS=all \
  -v "$adduser_volume:/igloo" \
  "$image" \
  /usr/local/bin/igloo-adduser -username check -password check-pass -platforms youtube >/dev/null

start_server() {
  compose up -d --pull never >/dev/null
}

wait_for_server() {
  for _ in $(seq 1 60); do
    if curl -fsS "${curl_local[@]}" "$base_url/api/health/live" >/dev/null 2>&1; then
      return
    fi
    sleep 1
  done

  curl -fsS "${curl_local[@]}" "$base_url/api/health/live" >/dev/null
}

login() {
  local cookie_file="$1"
  local login_html csrf status

  login_html="$(curl -fsS "${curl_local[@]}" -c "$cookie_file" "$base_url/login")"
  csrf="$(printf '%s\n' "$login_html" | sed -n 's/.*name="_csrf_token" value="\([^"]*\)".*/\1/p' | head -n1)"
  if [[ -z "$csrf" ]]; then
    echo "login page did not include CSRF token" >&2
    exit 1
  fi
  status="$(curl -fsS "${curl_local[@]}" -b "$cookie_file" -c "$cookie_file" \
    --data-urlencode "_csrf_token=$csrf" \
    --data-urlencode "username=check" \
    --data-urlencode "password=check-pass" \
    -o /dev/null -w '%{http_code}' \
    "$base_url/login")"
  if [[ "$status" != "303" ]]; then
    echo "login POST returned HTTP $status, want 303" >&2
    exit 1
  fi
}

start_server
wait_for_server
setup_html="$(curl -fsS "${curl_local[@]}" -c "$tmp/igloo-check-cookies.txt" "$base_url/setup")"
csrf="$(printf '%s\n' "$setup_html" | sed -n 's/.*name="_csrf_token" value="\([^"]*\)".*/\1/p' | head -n1)"
if [[ -z "$csrf" ]]; then
  echo "setup page did not include CSRF token" >&2
  exit 1
fi
status="$(curl -fsS "${curl_local[@]}" -b "$tmp/igloo-check-cookies.txt" -c "$tmp/igloo-check-cookies.txt" \
  --data-urlencode "_csrf_token=$csrf" \
  --data-urlencode "username=check" \
  --data-urlencode "password=check-pass" \
  --data-urlencode "password_confirm=check-pass" \
  --data-urlencode "platforms=youtube" \
  -o /dev/null -w '%{http_code}' \
  "$base_url/setup")"
if [[ "$status" != "303" ]]; then
  echo "setup POST returned HTTP $status, want 303" >&2
  exit 1
fi

curl -fsS "${curl_local[@]}" "$base_url/static/style.css" >/dev/null
login "$tmp/igloo-login-cookies.txt"

compose down >/dev/null
start_server
wait_for_server
login "$tmp/igloo-recreated-login-cookies.txt"

if runtime_is_podman "$runtime"; then
  echo "container check ok on $base_url using Podman through $runtime"
else
  echo "container check ok on $base_url using Docker through $runtime"
fi
