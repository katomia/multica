#!/bin/sh
set -eu

require_env() {
  name="$1"
  eval "value=\${$name:-}"
  if [ -z "$value" ]; then
    echo "missing required env: $name" >&2
    exit 1
  fi
}

require_env MULTICA_SERVER_URL
require_env MULTICA_WORKSPACE_ID
require_env MULTICA_RUNTIME_TOKEN

mkdir -p /root/.multica /root/.codex

if [ -f /opt/codex-config/config.toml ]; then
  cp /opt/codex-config/config.toml /root/.codex/config.toml
fi

cat >/root/.multica/config.json <<EOF
{
  "server_url": "${MULTICA_SERVER_URL}",
  "workspace_id": "${MULTICA_WORKSPACE_ID}",
  "token": "${MULTICA_RUNTIME_TOKEN}"
}
EOF
chmod 600 /root/.multica/config.json

exec /usr/local/bin/multica daemon start --foreground --no-auto-update
