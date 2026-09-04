#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"
revision="$(git rev-parse --verify "${1:-HEAD}^{commit}")"
target="git+file://$repo_root?rev=$revision#igloo.goModules"

# The flake includes the commit revision in the dependency output name, so a
# cached output belongs to these immutable inputs and can be safely reused.
nix build "$target" --no-link --print-build-logs
