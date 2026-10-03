#!/usr/bin/env sh

igloo_prepare_postgres() {
  igloo_postgres_root="$1"
  igloo_postgres_bundle="$igloo_postgres_root/bin/runtime/current/postgresql"
  igloo_postgres_source="${IGLOO_POSTGRES_BIN:-}"
  if [ -z "$igloo_postgres_source" ] && [ -x "$igloo_postgres_bundle/bin/initdb" ]; then
    igloo_postgres_source="$igloo_postgres_bundle/bin"
  fi
  if [ -z "$igloo_postgres_source" ]; then
    for igloo_postgres_directory in "${HOMEBREW_PREFIX:-/opt/homebrew}/opt/postgresql@18/bin" /usr/lib/postgresql/18/bin /usr/pgsql-18/bin /home/linuxbrew/.linuxbrew/opt/postgresql@18/bin /usr/local/opt/postgresql@18/bin /opt/local/lib/postgresql18/bin /Applications/Postgres.app/Contents/Versions/18/bin; do
      if [ -x "$igloo_postgres_directory/initdb" ]; then
        igloo_postgres_source="$igloo_postgres_directory"
        break
      fi
    done
  fi
  if [ -z "$igloo_postgres_source" ] && command -v initdb >/dev/null 2>&1; then
    igloo_postgres_source="$(dirname "$(command -v initdb)")"
  fi
  if [ -n "$igloo_postgres_source" ] && [ "$igloo_postgres_source" != "$igloo_postgres_bundle/bin" ]; then
    igloo_postgres_source="$(cd "$igloo_postgres_source" && pwd -P)" || return
  fi
  mkdir -p "$igloo_postgres_root/bin/runtime/current" || return
  case "$igloo_postgres_source" in
    /nix/store/*/bin)
      nix build "${igloo_postgres_source%/bin}" --out-link "$igloo_postgres_bundle" || return
      ;;
    "")
      if ! command -v nix >/dev/null 2>&1; then
        echo "Install PostgreSQL 18 or set IGLOO_POSTGRES_BIN." >&2
        return 1
      fi
      nix build "$igloo_postgres_root#postgresql" --out-link "$igloo_postgres_bundle" || return
      ;;
    "$igloo_postgres_bundle/bin")
      ;;
    */bin)
      rm -f "$igloo_postgres_bundle" || return
      ln -s "${igloo_postgres_source%/bin}" "$igloo_postgres_bundle" || return
      ;;
    *)
      mkdir -p "$igloo_postgres_bundle" || return
      rm -f "$igloo_postgres_bundle/bin" || return
      ln -s "$igloo_postgres_source" "$igloo_postgres_bundle/bin" || return
      ;;
  esac
  IGLOO_POSTGRES_BIN="$igloo_postgres_bundle/bin"
  export IGLOO_POSTGRES_BIN
  PATH="$IGLOO_POSTGRES_BIN:$PATH"
  export PATH
}
