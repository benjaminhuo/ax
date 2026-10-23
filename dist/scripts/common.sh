#!/usr/bin/env bash
# Shared configuration for the dist image build scripts.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

IMAGE_NAMESPACE="${IMAGE_NAMESPACE:-docker.io/benjaminhuo}"
TAG="${TAG:-latest}"
SERVER_IMAGE_REPO="${KO_DOCKER_REPO:-${IMAGE_NAMESPACE}/ax-server}"
TASK_RUNNER_REPO="${TASK_RUNNER_REPO:-${IMAGE_NAMESPACE}/ax-task-runner}"
DRY_RUN=false

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --dry-run) DRY_RUN=true ;;
      -h|--help)
        printf 'Usage: %s [--dry-run]\n\n%s\n' "$(basename "$0")" "${DESCRIPTION}"
        printf '\nPlatforms: linux/amd64, linux/arm64\n'
        printf 'Configuration: IMAGE_NAMESPACE, TAG, KO_DOCKER_REPO (server), TASK_RUNNER_REPO\n'
        printf 'Default images: docker.io/benjaminhuo/ax-server:latest, docker.io/benjaminhuo/ax-task-runner:latest\n'
        printf '\n--dry-run prints commands without building, pushing, or contacting registries.\n'
        exit 0
        ;;
      *) printf 'Unknown argument: %s\n' "$1" >&2; exit 2 ;;
    esac
    shift
  done
}

require_command() {
  if [[ "${DRY_RUN}" == false ]] && ! command -v "$1" >/dev/null 2>&1; then
    printf 'Required command not found: %s\n' "$1" >&2
    exit 1
  fi
}

run() {
  printf '+'
  printf ' %q' "$@"
  printf '\n'
  if [[ "${DRY_RUN}" == false ]]; then
    "$@"
  fi
}
