#!/usr/bin/env sh
set -eu
cd "$(dirname "$0")/../.."
. scripts/dev/postgres-tools.sh
igloo_prepare_postgres "$PWD"

if [ ! -x bin/igloo ]; then
  echo "Build Igloo before migrating the database." >&2
  exit 1
fi

systemctl --user stop igloo.service
bin/igloo migrate-sqlite
systemctl --user start igloo.service
