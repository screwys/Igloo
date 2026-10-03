#!/usr/bin/env sh
set -eu

if [ "$#" -eq 0 ]; then
  set -- igloo
fi

case "$1" in
  igloo|/usr/local/bin/igloo|igloo-adduser|/usr/local/bin/igloo-adduser)
    if [ "${2:-}" != migrate-sqlite ]; then
      igloo migrate-sqlite
    fi
    ;;
esac

exec "$@"
