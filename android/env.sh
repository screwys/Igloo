#!/bin/bash
# Shared Android build environment helpers.

set -euo pipefail

required_java_major=26

java_major_version() {
    local java_bin="$1"
    local version_line version

    version_line="$("$java_bin" -version 2>&1 | sed -n '1p')"
    version="$(printf '%s\n' "$version_line" | sed -n 's/.*version "\([^"]*\)".*/\1/p')"
    if [[ "$version" == 1.* ]]; then
        printf '%s\n' "${version#1.}" | cut -d. -f1
    else
        printf '%s\n' "$version" | cut -d. -f1
    fi
}

java_home_from_bin() {
    local java_bin="$1"
    local resolved

    resolved="$(readlink -f "$java_bin" 2>/dev/null || realpath "$java_bin" 2>/dev/null || printf '%s\n' "$java_bin")"
    dirname "$(dirname "$resolved")"
}

candidate_java_homes() {
    if [ -n "${JAVA_HOME:-}" ]; then
        printf '%s\n' "$JAVA_HOME"
    fi

    if command -v java >/dev/null 2>&1; then
        java_home_from_bin "$(command -v java)"
    fi

    printf '%s\n' \
        "$HOME/.sdkman/candidates/java/current" \
        "$HOME/.sdkman/candidates/java/${required_java_major}"* \
        "$HOME/.local/share/jdks/java-${required_java_major}-openjdk" \
        "$HOME/.local/share/jdks/jdk-${required_java_major}" \
        "/usr/lib/jvm/java-${required_java_major}-openjdk" \
        "/usr/lib/jvm/java-latest-openjdk" \
        "/usr/lib/jvm/jdk-${required_java_major}" \
        "/usr/lib/jvm/jdk-${required_java_major}-openjdk" \
        "/opt/homebrew/opt/openjdk@${required_java_major}" \
        "/opt/homebrew/opt/openjdk@${required_java_major}/libexec/openjdk.jdk/Contents/Home" \
        "/usr/local/opt/openjdk@${required_java_major}" \
        "/usr/local/opt/openjdk@${required_java_major}/libexec/openjdk.jdk/Contents/Home"
}

is_required_java_home() {
    local candidate="$1"
    local java_bin="$candidate/bin/java"
    local javac_bin="$candidate/bin/javac"
    local major

    [ -x "$java_bin" ] || return 1
    [ -x "$javac_bin" ] || return 1
    major="$(java_major_version "$java_bin")"
    [ "$major" = "$required_java_major" ]
}

require_java_home() {
    local candidate
    local original_java_home="${JAVA_HOME:-}"

    while IFS= read -r candidate; do
        [ -n "$candidate" ] || continue
        if is_required_java_home "$candidate"; then
            export JAVA_HOME="$candidate"
            export PATH="$JAVA_HOME/bin:$PATH"
            return 0
        fi
    done < <(candidate_java_homes | awk 'NF && !seen[$0]++')

    if [ -n "$original_java_home" ]; then
        echo "❌ JAVA_HOME is set, but it is not a Java ${required_java_major} JDK: $original_java_home"
    else
        echo "❌ Java ${required_java_major} JDK not found."
    fi
    echo "   Set JAVA_HOME to a Java ${required_java_major} JDK, put Java ${required_java_major} on PATH,"
    echo "   or install it with your platform package manager."
    echo "   Examples:"
    echo "     Arch/CachyOS: sudo pacman -S jdk-openjdk"
    echo "     Fedora:       rpm-ostree install java-latest-openjdk-devel"
    echo "     Homebrew:     brew install openjdk@${required_java_major}"
    return 1
}
