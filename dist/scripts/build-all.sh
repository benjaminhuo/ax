#!/usr/bin/env bash
# Build and push both AX images; never applies Kubernetes manifests.

set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

DESCRIPTION='Build and push both AX images for amd64 and arm64.'
parse_args "$@"
require_command go
require_command ko
require_command docker

if [[ "${DRY_RUN}" == false ]]; then
  docker buildx version >/dev/null
  docker info >/dev/null
fi

bash "${SCRIPT_DIR}/build-ax-server.sh" "$@"
bash "${SCRIPT_DIR}/build-ax-task-runner.sh" "$@"
