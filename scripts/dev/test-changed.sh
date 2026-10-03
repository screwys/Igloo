#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

if [[ -n "${IGLOO_TEST_BASE:-}" || -n "${IGLOO_TEST_HEAD:-}" ]]; then
  if [[ -z "${IGLOO_TEST_BASE:-}" || -z "${IGLOO_TEST_HEAD:-}" ]]; then
    echo "IGLOO_TEST_BASE and IGLOO_TEST_HEAD must be set together" >&2
    exit 2
  fi
  mapfile -t changed < <(
    git diff --name-only --no-renames --diff-filter=ACDM "$IGLOO_TEST_BASE" "$IGLOO_TEST_HEAD" |
      sed '/^$/d' |
      sort -u
  )
else
  mapfile -t changed < <(
    {
      git diff --name-only --no-renames --diff-filter=ACDM HEAD
      git ls-files --others --exclude-standard
    } | sed '/^$/d' | sort -u
  )
fi

if [[ "${#changed[@]}" -eq 0 ]]; then
  echo "[test] no changed files; use 'just test-full' for the exhaustive gate"
  exit 0
fi

printf '[test] checking %d changed files\n' "${#changed[@]}"

go_changed=0
go_full=0
drift_changed=0
web_changed=0
android_changed=0
i18n_changed=0
workflow_changed=0
contract_changed=0
declare -a shell_files=()
declare -a node_tests=()
declare -a go_files=()

for path in "${changed[@]}"; do
  case "$path" in
    *.go|*.templ)
      go_changed=1
      go_files+=("$path")
      ;;
  esac

  case "$path" in
    go.mod|go.sum|sqlc.yaml|internal/db/queries/*|internal/db/query/*|internal/db/postgres/*|.golangci.yml|.githooks/pre-push|scripts/dev/lint-go.sh|scripts/dev/go-tool-versions.sh|scripts/dev/test-changed.sh|scripts/dev/changed-go/*)
      go_changed=1
      go_full=1
      ;;
  esac

  case "$path" in
    internal/db/*.go|internal/db/queries/*|internal/db/postgres/*|sqlc.yaml|internal/model/*.go|internal/web/*.go)
      contract_changed=1
      ;;
  esac

  case "$path" in
    sqlc.yaml|internal/db/queries/*|internal/db/postgres/*|internal/db/query/*|*.templ|internal/components/*|static/js/src/*|static/style.css|locales/*)
      drift_changed=1
      ;;
  esac

  case "$path" in
    internal/web/*|internal/components/*|static/js/*|static/style.css|locales/*)
      web_changed=1
      ;;
  esac

  case "$path" in
    locales/*|android/app/src/main/res/values/strings.xml)
      i18n_changed=1
      ;;
  esac

  case "$path" in
    android/app/src/main/res/values/strings.xml)
      ;;
    android/*)
      android_changed=1
      ;;
  esac

  case "$path" in
    .github/workflows/*|.github/actions/*)
      workflow_changed=1
      ;;
  esac

  case "$path" in
    *.sh)
      if [[ -f "$path" ]]; then shell_files+=("$path"); fi
      ;;
    *.test.mjs)
      if [[ -f "$path" ]]; then node_tests+=("$path"); fi
      ;;
  esac
done

if [[ "${IGLOO_TEST_SELECTION_ONLY:-0}" == "1" ]]; then
  printf 'go=%d drift=%d web=%d i18n=%d android=%d workflow=%d contract=%d\n' \
    "$go_changed" "$drift_changed" "$web_changed" "$i18n_changed" "$android_changed" "$workflow_changed" "$contract_changed"
  exit 0
fi

if [[ "$go_changed" -eq 1 || "$web_changed" -eq 1 ]] && [[ -z "${IGLOO_POSTGRES_BIN:-}" ]] && ! command -v initdb >/dev/null 2>&1 && command -v nix >/dev/null 2>&1; then
  exec nix shell --impure .#postgresql --command bash "$0" "$@"
fi

if [[ "${#shell_files[@]}" -gt 0 ]]; then
  echo "[shell] checking changed scripts"
  for path in "${shell_files[@]}"; do
    bash -n "$path"
  done
fi

if [[ "$workflow_changed" -eq 1 ]]; then
  echo "[actions] running actionlint"
  . scripts/dev/go-tool-versions.sh
  go run "github.com/rhysd/actionlint/cmd/actionlint@${ACTIONLINT_VERSION}"
fi

if [[ "$drift_changed" -eq 1 ]]; then
  echo "[drift] regenerating tracked web outputs"
  scripts/dev/drift-check.sh --write
fi

if [[ "$i18n_changed" -eq 1 ]]; then
  echo "[i18n] checking generated catalog outputs"
  go test ./scripts/dev/i18n_sync_catalog -run TestGeneratedCatalogOutputsAreCurrent -count=1
fi

if [[ "$go_changed" -eq 1 ]]; then
  if [[ "$go_full" -eq 1 ]]; then
    go_packages=(./...)
  else
    package_selection="$(go run ./scripts/dev/changed-go/main.go packages "${go_files[@]}")"
    mapfile -t go_packages <<<"$package_selection"
  fi
  printf '[go] running tests for %s\n' "${go_packages[*]}"
  go test -timeout 30m "${go_packages[@]}"

  . scripts/dev/go-tool-versions.sh
  echo "[go] running repo-specific static checks"
  go run ./scripts/dev/staticcheck
  echo "[go] running golangci-lint"
  scripts/dev/lint-go.sh run
  echo "[go] running govulncheck"
  go run "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}" ./...

fi

if [[ "${#node_tests[@]}" -gt 0 ]]; then
  echo "[node] running changed behavior tests"
  node --test "${node_tests[@]}"
fi

if [[ "$web_changed" -eq 1 ]]; then
  echo "[web] running process-level test"
  scripts/dev/web-test.sh
fi

if [[ "$android_changed" -eq 1 ]]; then
  echo "[android] running JVM tests"
  android/test.sh
fi

git diff --check
echo "[test] proportional gate passed"
