#!/usr/bin/env bash
# Build and push both runner architectures, then publish their image index.

set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

DESCRIPTION='Build and push ax-task-runner for amd64 and arm64 using Go and Docker Buildx.'
parse_args "$@"
require_command go
require_command docker
cd "${REPO_ROOT}"

if [[ "${DRY_RUN}" == false ]]; then
  docker buildx version >/dev/null
  docker info >/dev/null
fi

for arch in amd64 arm64; do
  run mkdir -p "bin/linux_${arch}"
  run env GOOS=linux GOARCH="${arch}" CGO_ENABLED=0 \
    go build -trimpath -ldflags='-s -w' \
    -o "bin/linux_${arch}/ax-task-runner" ./cmd/ax-task-runner
done

for arch in amd64 arm64; do
  run docker buildx build \
    --platform "linux/${arch}" \
    --build-arg "TARGETARCH=${arch}" \
    --tag "${TASK_RUNNER_REPO}:${TAG}-${arch}" \
    --file Dockerfile.task-runner \
    --push .
done

run docker buildx imagetools create \
  --tag "${TASK_RUNNER_REPO}:${TAG}" \
  "${TASK_RUNNER_REPO}:${TAG}-amd64" \
  "${TASK_RUNNER_REPO}:${TAG}-arm64"
