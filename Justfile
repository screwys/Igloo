set default-list

postgres_bin := ```
    if [ -n "${IGLOO_POSTGRES_BIN:-}" ]; then
      printf '%s' "$IGLOO_POSTGRES_BIN"
    else
      for directory in "${IGLOO_RUNTIME_DIR:-$PWD/runtime/current}/postgresql/bin" "$PWD/bin/runtime/current/postgresql/bin" "${HOMEBREW_PREFIX:-/opt/homebrew}/opt/postgresql@18/bin" /usr/lib/postgresql/18/bin /usr/pgsql-18/bin /home/linuxbrew/.linuxbrew/opt/postgresql@18/bin /usr/local/opt/postgresql@18/bin /opt/local/lib/postgresql18/bin /Applications/Postgres.app/Contents/Versions/18/bin; do
        if [ -x "$directory/initdb" ]; then
          printf '%s' "$directory"
          exit 0
        fi
      done
      if command -v initdb >/dev/null 2>&1; then
        dirname "$(command -v initdb)"
      fi
    fi
    ```

# Build the server binary and generated web assets without restarting it.
build:
    #!/usr/bin/env bash
    set -euo pipefail
    GOCACHE="${GOCACHE:-$PWD/.local/go-cache}" scripts/dev/build.sh

# Stop the local service, migrate its SQLite database, and restart it.
migrate-database:
    GOCACHE="${GOCACHE:-$PWD/.local/go-cache}" go build -o bin/igloo ./cmd/igloo
    sh scripts/dev/migrate-database.sh

# Build the server and restart the local service.
restart:
    #!/usr/bin/env bash
    set -euo pipefail
    GOCACHE="${GOCACHE:-$PWD/.local/go-cache}" scripts/dev/build.sh restart

# Build the server, reload its systemd unit, and restart the local service.
restart-daemon:
    #!/usr/bin/env bash
    set -euo pipefail
    GOCACHE="${GOCACHE:-$PWD/.local/go-cache}" scripts/dev/build.sh full

# Build server assets and install/relaunch the Android app on a connected device.
build-android-with-server:
    just build
    just build-android

# Build and restart the server, then install/relaunch the Android app.
restart-and-build-android:
    just restart
    just build-android

# Run the proportional local gate for files changed from HEAD.
test:
    IGLOO_POSTGRES_BIN={{ quote(postgres_bin) }} GOCACHE="${GOCACHE:-$PWD/.local/go-cache}" scripts/dev/test-changed.sh

# Run every repository gate; stale generated files may be regenerated before the check.
test-full:
    IGLOO_POSTGRES_BIN={{ quote(postgres_bin) }} GOCACHE="${GOCACHE:-$PWD/.local/go-cache}" scripts/dev/test-full.sh

# Run the Go test suite only.
test-go:
    #!/usr/bin/env bash
    set -euo pipefail
    export IGLOO_POSTGRES_BIN={{ quote(postgres_bin) }}
    if [ -z "$IGLOO_POSTGRES_BIN" ] && command -v nix >/dev/null 2>&1; then exec nix shell --impure .#postgresql --command just test-go; fi
    GOCACHE="${GOCACHE:-$PWD/.local/go-cache}" go test -timeout 30m ./...

# Run the Go test suite with the race detector.
test-go-race:
    #!/usr/bin/env bash
    set -euo pipefail
    export IGLOO_POSTGRES_BIN={{ quote(postgres_bin) }}
    if [ -z "$IGLOO_POSTGRES_BIN" ] && command -v nix >/dev/null 2>&1; then exec nix shell --impure .#postgresql --command just test-go-race; fi
    GOCACHE="${GOCACHE:-$PWD/.local/go-cache}" go test -race -timeout 30m ./...

# Run the pinned Go linters and formatting checks.
lint-go:
    GOCACHE="${GOCACHE:-$PWD/.local/go-cache}" scripts/dev/lint-go.sh run

# Format Go source with the pinned formatter.
fmt-go:
    scripts/dev/lint-go.sh fmt

# Run Go tests for one package, optionally matching a test-name regexp.
test-go-package package filter="":
    #!/usr/bin/env bash
    set -euo pipefail
    export IGLOO_POSTGRES_BIN={{ quote(postgres_bin) }}
    if [ -z "$IGLOO_POSTGRES_BIN" ] && command -v nix >/dev/null 2>&1; then exec nix shell --impure .#postgresql --command just test-go-package {{ quote(package) }} {{ quote(filter) }}; fi
    if [ -n {{ quote(filter) }} ]; then GOCACHE="${GOCACHE:-$PWD/.local/go-cache}" go test -timeout 30m {{ quote(package) }} -run {{ quote(filter) }} -count=1; else GOCACHE="${GOCACHE:-$PWD/.local/go-cache}" go test -timeout 30m {{ quote(package) }}; fi

# Run Android JVM tests, optionally for one class: just test-android com.example.Test
test-android filter="":
    if [ -n "{{ filter }}" ]; then android/test.sh {{ quote(filter) }}; else android/test.sh; fi

# Build, install, and relaunch the Android app on a connected device.
build-android:
    #!/usr/bin/env bash
    set -euo pipefail
    output="$(mktemp)"
    trap 'rm -f "$output"' EXIT
    if android/build.sh >"$output" 2>&1; then status=0; else status=$?; fi
    cat "$output"
    if (( status != 0 )); then exit "$status"; fi
    if grep -Fq 'Skipping install.' "$output"; then
        printf '%s\n' 'android/build.sh did not install or relaunch the app because adb is unavailable.' >&2
        exit 1
    fi

# Build the Android APK without installing it.
android-apk:
    android/build.sh apk

# Compile Android Kotlin without assembling or installing an APK.
android-compile:
    android/build.sh compile

# Run the throwaway-server web test.
test-web:
    #!/usr/bin/env bash
    set -euo pipefail
    export IGLOO_POSTGRES_BIN={{ quote(postgres_bin) }}
    if [ -z "$IGLOO_POSTGRES_BIN" ] && command -v nix >/dev/null 2>&1; then exec nix shell --impure .#postgresql --command just test-web; fi
    GOCACHE="${GOCACHE:-$PWD/.local/go-cache}" scripts/dev/web-test.sh

# Regenerate sqlc bindings, templ, and bundled assets.
check-drift:
    GOCACHE="${GOCACHE:-$PWD/.local/go-cache}" scripts/dev/drift-check.sh --write

# Verify the Nix dependency hash for a commit (defaults to HEAD).
check-nix-deps revision="":
    bash scripts/dev/check-nix-deps.sh {{ quote(revision) }}

# Build both downloader tools from current upstream HEAD.
build-downloaders:
    nix build --impure --refresh .#yt-dlp .#gallery-dl --no-link --print-out-paths

# Validate the native schema, legacy archive, and Android Room contracts.
check-schema:
    #!/usr/bin/env bash
    set -euo pipefail
    export IGLOO_POSTGRES_BIN={{ quote(postgres_bin) }}
    if [ -z "$IGLOO_POSTGRES_BIN" ] && command -v nix >/dev/null 2>&1; then exec nix shell --impure .#postgresql --command just check-schema; fi
    GOCACHE="${GOCACHE:-$PWD/.local/go-cache}" scripts/dev/schema-check.sh

# Regenerate the server schema snapshot.
update-schema-snapshot:
    #!/usr/bin/env bash
    set -euo pipefail
    export IGLOO_POSTGRES_BIN={{ quote(postgres_bin) }}
    if [ -z "$IGLOO_POSTGRES_BIN" ] && command -v nix >/dev/null 2>&1; then exec nix shell --impure .#postgresql --command just update-schema-snapshot; fi
    GOCACHE="${GOCACHE:-$PWD/.local/go-cache}" go test ./internal/db -run TestSchemaSnapshot -update -count=1

# Build an image and exercise its basic container runtime contract.
check-container:
    scripts/dev/container-check.sh

# Verify that the shared catalog and generated Android resources are current.
i18n-check:
    GOCACHE="${GOCACHE:-$PWD/.local/go-cache}" go test ./scripts/dev/i18n_sync_catalog -run TestGeneratedCatalogOutputsAreCurrent -count=1

# Regenerate the shared catalog and Android string resources.
i18n-sync:
    GOCACHE="${GOCACHE:-$PWD/.local/go-cache}" go run ./scripts/dev/i18n_sync_catalog

# Report runtime paths, cache state, and local health through the Igloo doctor.
doctor:
    GOCACHE="${GOCACHE:-$PWD/.local/go-cache}" scripts/dev/doctor.sh

# Check the working-tree diff for whitespace errors.
diff-check:
    git diff --check

# Build the Windows setup executable from prepared application and runtime folders.
build-windows-installer version app runtime output:
    pwsh -NoProfile -File packaging/windows/build-installer.ps1 -ProductVersion {{ quote(version) }} -AppDirectory {{ quote(app) }} -RuntimeDirectory {{ quote(runtime) }} -OutputDirectory {{ quote(output) }}

# Exercise installation, service setup, and uninstall on a disposable Windows host.
test-windows-installer installer:
    powershell.exe -NoProfile -File packaging/windows/test-installer.ps1 -Installer {{ quote(installer) }}

# Create, publish, and dispatch a signed release after an explicit request with a user-written summary.
release bump summary:
    .github/scripts/create-release-tag.sh --push {{ quote(bump) }} {{ quote(summary) }}

# Create a local signed release tag without publishing it.
release-local bump summary:
    .github/scripts/create-release-tag.sh {{ quote(bump) }} {{ quote(summary) }}
