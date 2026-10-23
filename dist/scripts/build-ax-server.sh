#!/usr/bin/env bash
# Build and push the multi-platform AX server image with ko.

set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

DESCRIPTION='Build and push ax-server for amd64 and arm64 using ko.'
parse_args "$@"
require_command go
require_command ko
cd "${REPO_ROOT}"

run env KO_DOCKER_REPO="${SERVER_IMAGE_REPO}" ko build \
  --bare \
  --platform=linux/amd64,linux/arm64 \
  --tags="${TAG}" \
  ./cmd/ax-server
