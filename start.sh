#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

if command -v go >/dev/null 2>&1; then
  calendar_go="$(command -v go)"
elif [ -x "${XDG_CACHE_HOME:-$HOME/.cache}/space-calendar/go/bin/go" ]; then
  calendar_go="${XDG_CACHE_HOME:-$HOME/.cache}/space-calendar/go/bin/go"
else
  echo "Для запуска установите Go 1.22 или новее: https://go.dev/dl/"
  exit 1
fi

exec "$calendar_go" run . "$@"
