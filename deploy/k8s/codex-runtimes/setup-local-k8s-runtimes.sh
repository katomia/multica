#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$ROOT"

NAMESPACE="${NAMESPACE:-omnigent}"
GO_NODE="${GO_NODE:-ht-calicok8swork-01}"
PYTHON_NODE="${PYTHON_NODE:-ht-calicok8swork-03}"
LOCAL_SERVER="${LOCAL_SERVER:-http://127.0.0.1:8080}"
IMAGE_REPO="${IMAGE_REPO:-harbor.weizhipin.com/lc-test/multica-runtime}"
TAG="${TAG:-$(date +%Y%m%d-%H%M%S)}"
GO_IMAGE="${GO_IMAGE:-${IMAGE_REPO}:codex-go-${TAG}}"
PYTHON_IMAGE="${PYTHON_IMAGE:-${IMAGE_REPO}:codex-python-${TAG}}"

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing dependency: $1" >&2
    exit 1
  }
}

need curl
need jq
need docker
need kubectl
need envsubst

kubectl_clean() {
  env -u HTTP_PROXY -u HTTPS_PROXY -u http_proxy -u https_proxy kubectl "$@"
}

detect_lan_ip() {
  if command -v ipconfig >/dev/null 2>&1; then
    for iface in en0 en1; do
      if ip="$(ipconfig getifaddr "$iface" 2>/dev/null)" && [ -n "$ip" ]; then
        echo "$ip"
        return 0
      fi
    done
  fi
  hostname -I 2>/dev/null | awk '{print $1}'
}

if ! curl -fsS "${LOCAL_SERVER}/health" >/dev/null; then
  echo "local multica server is not healthy at ${LOCAL_SERVER}" >&2
  exit 1
fi

LAN_IP="$(detect_lan_ip)"
if [ -z "${LAN_IP}" ]; then
  echo "failed to detect local LAN IP" >&2
  exit 1
fi
MULTICA_SERVER_URL="${MULTICA_SERVER_URL:-http://${LAN_IP}:8080}"

JWT_TOKEN="$(
  curl -fsS -X POST "${LOCAL_SERVER}/auth/dev-login" \
    -H 'content-type: application/json' \
    -d '{}' | jq -r '.token'
)"

WORKSPACE_JSON="$(
  curl -fsS "${LOCAL_SERVER}/api/workspaces" \
    -H "Authorization: Bearer ${JWT_TOKEN}"
)"
MULTICA_WORKSPACE_ID="${MULTICA_WORKSPACE_ID:-$(printf '%s' "$WORKSPACE_JSON" | jq -r '.[0].id')}"
if [ -z "${MULTICA_WORKSPACE_ID}" ] || [ "${MULTICA_WORKSPACE_ID}" = "null" ]; then
  echo "failed to resolve local workspace id" >&2
  exit 1
fi

MULTICA_RUNTIME_TOKEN="$(
  curl -fsS -X POST "${LOCAL_SERVER}/api/tokens" \
    -H "Authorization: Bearer ${JWT_TOKEN}" \
    -H 'content-type: application/json' \
    -d '{"name":"k8s-runtime","expires_in_days":30}' | jq -r '.token'
)"

(
  cd server
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/multica ./cmd/multica
)

docker buildx build \
  --platform linux/amd64 \
  -f deploy/k8s/codex-runtimes/Dockerfile.codex-go \
  -t "${GO_IMAGE}" \
  --push \
  .

docker buildx build \
  --platform linux/amd64 \
  -f deploy/k8s/codex-runtimes/Dockerfile.codex-python \
  -t "${PYTHON_IMAGE}" \
  --push \
  .

export NAMESPACE GO_NODE PYTHON_NODE GO_IMAGE PYTHON_IMAGE MULTICA_SERVER_URL MULTICA_RUNTIME_TOKEN MULTICA_WORKSPACE_ID
envsubst < deploy/k8s/codex-runtimes/manifest.yaml.tpl | kubectl_clean apply -f -

kubectl_clean -n "${NAMESPACE}" rollout status deploy/multica-codex-go-runtime --timeout=5m
kubectl_clean -n "${NAMESPACE}" rollout status deploy/multica-codex-python-runtime --timeout=5m

RUNTIMES_JSON="$(
  curl -fsS "${LOCAL_SERVER}/api/runtimes" \
    -H "Authorization: Bearer ${JWT_TOKEN}" \
    -H "X-Workspace-Id: ${MULTICA_WORKSPACE_ID}"
)"

printf '%s\n' "$RUNTIMES_JSON" | jq '[.[] | {name, provider, status, runtime_mode}]'

echo
echo "images:"
echo "  ${GO_IMAGE}"
echo "  ${PYTHON_IMAGE}"
echo "workspace_id: ${MULTICA_WORKSPACE_ID}"
echo "server_url: ${MULTICA_SERVER_URL}"
