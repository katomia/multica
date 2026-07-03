#!/usr/bin/env bash
set -euo pipefail

need_env() {
  local name="$1"
  if [ -z "${!name:-}" ]; then
    echo "missing required env: $name" >&2
    exit 1
  fi
}

need_env MULTICA_SERVER_URL
need_env MULTICA_WORKSPACE_ID
need_env MULTICA_RUNTIME_TOKEN
need_env MULTICA_BINARY_URL
need_env RUNTIME_FLAVOR

export PATH="/root/.local/bin:${PATH}"

profile_args=()
config_dir="/root/.multica"
if [ -n "${MULTICA_PROFILE:-}" ]; then
  config_dir="/root/.multica/profiles/${MULTICA_PROFILE}"
  mkdir -p "${config_dir}"
  profile_args=(--profile "${MULTICA_PROFILE}")
fi

if [ "${RUNTIME_FLAVOR}" = "go" ] && ! command -v go >/dev/null 2>&1; then
  apt-get update
  apt-get install -y --no-install-recommends golang-go
  if [ -x /usr/lib/go-1.19/bin/go ]; then
    ln -sf /usr/lib/go-1.19/bin/go /usr/local/bin/go
  fi
fi

mkdir -p /root/.codex /root/.multica /usr/local/bin "${config_dir}"
cp /opt/codex-config/config.toml /root/.codex/config.toml

if [ -f /opt/zhishu-docs/zhishu-docs.py ]; then
  mkdir -p /root/.local/bin
  install -m 0755 /opt/zhishu-docs/zhishu-docs.py /root/.local/bin/zhishu-docs
fi

if [ -n "${ZHISHU_APIKEY:-}" ]; then
  mkdir -p /root/.boss-ai
  printf 'ZHISHU_APIKEY=%s\n' "${ZHISHU_APIKEY}" > /root/.boss-ai/zhishu_apikey
  chmod 600 /root/.boss-ai/zhishu_apikey
fi

curl -fsSL "${MULTICA_BINARY_URL}" -o /usr/local/bin/multica
chmod +x /usr/local/bin/multica

# Keep these K8s runtimes Codex-only so the workspace shows the intended
# development targets instead of every CLI baked into the base image.
for extra in claude pi; do
  if extra_path="$(command -v "$extra" 2>/dev/null)"; then
    mv "$extra_path" "${extra_path}.disabled"
  fi
done

cat > "${config_dir}/config.json" <<EOF
{
  "server_url": "${MULTICA_SERVER_URL}",
  "workspace_id": "${MULTICA_WORKSPACE_ID}",
  "token": "${MULTICA_RUNTIME_TOKEN}"
}
EOF

exec /usr/local/bin/multica "${profile_args[@]}" daemon start --foreground --no-auto-update
