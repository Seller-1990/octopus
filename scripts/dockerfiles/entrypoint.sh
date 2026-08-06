#!/bin/sh
set -e

PUID="${PUID:-0}"
PGID="${PGID:-0}"
DATA_DIR="${OCTOPUS_DATA_DIR:-/app/data}"
UPDATED_BIN="$DATA_DIR/octopus-updated"
ORIGINAL_BIN="/app/octopus"

# Determine which binary to use
BIN="$ORIGINAL_BIN"
if [ -x "$UPDATED_BIN" ]; then
    BIN="$UPDATED_BIN"
fi

chmod +x "$BIN"

if [ "$PUID" != "0" ] || [ "$PGID" != "0" ]; then
    chown -R "$PUID:$PGID" /app
    [ -d "$DATA_DIR" ] && chown -R "$PUID:$PGID" "$DATA_DIR"
fi

cd /app

if command -v su-exec >/dev/null 2>&1; then
    exec su-exec "$PUID:$PGID" "$BIN" "$@"
elif command -v gosu >/dev/null 2>&1; then
    exec gosu "$PUID:$PGID" "$BIN" "$@"
else
    if [ "$PUID" != "0" ] || [ "$PGID" != "0" ]; then
        echo "Warning: neither su-exec nor gosu is available; running as root." >&2
    fi
    exec "$BIN" "$@"
fi
