#!/bin/bash
# Igloo Android — JVM unit tests (no device, no app data touched).
#
# Usage:
#   ./test.sh                          # run all devtest JVM unit tests
#   ./test.sh com.screwy.igloo.data.IglooDatabaseTest  # one class
#   ./test.sh 'com.screwy.igloo.data.*'                 # wildcard

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

source "$SCRIPT_DIR/env.sh"
require_java_home
ensure_android_sdk

export GRADLE_USER_HOME="${GRADLE_USER_HOME:-$SCRIPT_DIR/.gradle-home}"
mkdir -p "$GRADLE_USER_HOME"

if [ "${IGLOO_ANDROID_SCRIPT_LOCK_HELD:-}" != "1" ]; then
    mkdir -p "$SCRIPT_DIR/.gradle-home"
    export IGLOO_ANDROID_SCRIPT_LOCK_HELD=1
    exec flock --close "$SCRIPT_DIR/.gradle-home/igloo-android.lock" "$SCRIPT_DIR/$(basename "$0")" "$@"
fi

test_args=()
if [ "${IGLOO_ANDROID_CLEAN:-0}" = "1" ]; then
    test_args+=("clean")
fi
test_args+=(":app:testDevtestUnitTest")
if [ "${IGLOO_ANDROID_RERUN_TASKS:-0}" = "1" ]; then
    test_args+=("--rerun-tasks")
fi
if [ "${IGLOO_ANDROID_NO_DAEMON:-0}" = "1" ]; then
    test_args+=("--no-daemon")
fi
if [ $# -gt 0 ]; then
    test_args+=("--tests" "$@")
fi

echo "🧪 Running unit tests..."
echo "   JAVA_HOME=$JAVA_HOME"
test_log="$(mktemp)"
cleanup() {
    rm -f "$test_log"
}
trap cleanup EXIT

set +e
./gradlew "${test_args[@]}" 2>&1 | tee "$test_log"
gradle_status="${PIPESTATUS[0]}"
set -e
if [ "$gradle_status" -ne 0 ]; then
    test_results_dir="$SCRIPT_DIR/app/build/test-results/testDevtestUnitTest"
    while IFS= read -r -d '' result_file; do
        if ! grep -q '<failure' "$result_file"; then
            continue
        fi
        echo "" >&2
        echo "❌ Failure details from ${result_file#"$SCRIPT_DIR/"}:" >&2
        sed -n '/<failure/,/<\/failure>/p' "$result_file" >&2
    done < <(find "$test_results_dir" -name 'TEST-*.xml' -print0 2>/dev/null)
    exit "$gradle_status"
fi

final_field_warnings="$(grep -E '^WARNING: Final field .* has been mutated reflectively|^WARNING: Mutating final fields will be blocked' "$test_log" || true)"
if [ -n "$final_field_warnings" ]; then
    echo ""
    echo "❌ JVM final-field mutation warning emitted during Android tests." >&2
    echo "   Replace concrete-class mocks with fakes/interfaces or update the dependency causing it." >&2
    printf '%s\n' "$final_field_warnings" >&2
    exit 1
fi

test_results_dir="$SCRIPT_DIR/app/build/test-results/testDevtestUnitTest"
async_teardown_errors="$({
    grep -R -E 'UncaughtExceptionsBeforeTest|Cannot perform this operation because the connection pool has been closed' \
        "$test_results_dir" --include='*.xml' 2>/dev/null || true
} | head -20)"
if [ -n "$async_teardown_errors" ]; then
    echo "" >&2
    echo "❌ Android tests left an asynchronous failure after test teardown." >&2
    printf '%s\n' "$async_teardown_errors" >&2
    exit 1
fi

echo ""
echo "✅ All tests passed!"
