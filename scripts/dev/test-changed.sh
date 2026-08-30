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
    git diff --name-only --diff-filter=ACMR "$IGLOO_TEST_BASE" "$IGLOO_TEST_HEAD" |
      sed '/^$/d' |
      sort -u
  )
else
  mapfile -t changed < <(
    {
      git diff --name-only --diff-filter=ACMR HEAD
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
drift_changed=0
web_changed=0
android_changed=0
i18n_changed=0
workflow_changed=0
contract_changed=0
declare -A go_test_file_set=()
declare -a shell_files=()
declare -a node_tests=()

for path in "${changed[@]}"; do
  case "$path" in
    *.go)
      go_changed=1
      if [[ "$path" == *_test.go ]]; then
        go_test_file_set["$path"]=1
      fi
      ;;
    go.mod|go.sum)
      go_changed=1
      ;;
  esac

  case "$path" in
    internal/db/*.go|internal/model/*.go|internal/web/*.go)
      contract_changed=1
      go_test_file_set["internal/web/contract_test.go"]=1
      ;;
  esac

  case "$path" in
    *.templ|internal/components/*|static/js/src/*|static/style.css|locales/*)
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
      shell_files+=("$path")
      ;;
    *.test.mjs)
      node_tests+=("$path")
      ;;
  esac
done

if [[ "${IGLOO_TEST_SELECTION_ONLY:-0}" == "1" ]]; then
  printf 'go=%d drift=%d web=%d i18n=%d android=%d workflow=%d contract=%d\n' \
    "$go_changed" "$drift_changed" "$web_changed" "$i18n_changed" "$android_changed" "$workflow_changed" "$contract_changed"
  exit 0
fi

if [[ "${#shell_files[@]}" -gt 0 ]]; then
  echo "[shell] checking changed scripts"
  bash -n "${shell_files[@]}"
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
  echo "[go] compiling all packages"
  go test -run '^$' ./...

  . scripts/dev/go-tool-versions.sh
  echo "[go] running repo-specific static checks"
  go run ./scripts/dev/staticcheck
  echo "[go] running errcheck"
  go run "github.com/kisielk/errcheck@${ERRCHECK_VERSION}" ./...
  echo "[go] running staticcheck"
  go run "honnef.co/go/tools/cmd/staticcheck@${STATICCHECK_VERSION}" ./...
  echo "[go] running govulncheck"
  go run "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}" ./...

  declare -A go_tests_by_package=()
  for test_file in "${!go_test_file_set[@]}"; do
    dir="${test_file%/*}"
    [[ "$dir" == "$test_file" ]] && dir="."
    package="./$dir"
    while IFS= read -r test_name; do
      [[ -z "$test_name" ]] && continue
      if [[ -n "${go_tests_by_package[$package]:-}" ]]; then
        go_tests_by_package["$package"]+="|"
      fi
      go_tests_by_package["$package"]+="$test_name"
    done < <(
      sed -nE 's/^[[:space:]]*func[[:space:]]+(Test[A-Za-z0-9_]+|Example[A-Za-z0-9_]*|Fuzz[A-Za-z0-9_]+)[[:space:]]*\(.*/\1/p' "$test_file"
    )
  done

  if [[ "${#go_tests_by_package[@]}" -eq 0 ]]; then
    echo "[go] no changed Go test files"
  else
    mapfile -t go_test_packages < <(printf '%s\n' "${!go_tests_by_package[@]}" | sort)
    for package in "${go_test_packages[@]}"; do
      echo "[go] testing $package: ${go_tests_by_package[$package]//|/, }"
      go test -count=1 -run "^(${go_tests_by_package[$package]})$" "$package"
    done
  fi
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
  if [[ -n "${IGLOO_TEST_BASE:-}" ]]; then
    IGLOO_ANDROID_RERUN_TASKS=1 android/test.sh
  else
    android/test.sh
  fi
fi

git diff --check
echo "[test] proportional gate passed"
