#!/usr/bin/env bash
# stop-mcpjungle.sh — Stop MCPJungle Docker Compose on the host.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

source "$REPO_ROOT/scripts/utils/logging.sh"

[[ -f "$REPO_ROOT/.env" ]] && set -a && source "$REPO_ROOT/.env" && set +a

COLIMA_PROFILE="${AIVM_COLIMA_PROFILE:-aivm}"
COMPOSE_FILE="$REPO_ROOT/docker-compose.mcpjungle.yml"

find_host_docker_socket_stop() {
  local aivm_sock="$HOME/.colima/${COLIMA_PROFILE}/docker.sock"

  if [[ -S "/var/run/docker.sock" ]]; then
    local realpath_sock
    realpath_sock=$(readlink -f /var/run/docker.sock 2>/dev/null || echo "/var/run/docker.sock")
    [[ "$realpath_sock" != "${aivm_sock}" ]] && echo "unix:///var/run/docker.sock" && return 0
  fi
  [[ -S "$HOME/.orbstack/run/docker.sock" ]] \
    && echo "unix://$HOME/.orbstack/run/docker.sock" && return 0
  [[ -S "$HOME/.colima/default/docker.sock" ]] \
    && echo "unix://$HOME/.colima/default/docker.sock" && return 0
  for sock_path in "$HOME"/.colima/*/docker.sock; do
    [[ -S "$sock_path" ]] || continue
    local profile_name
    profile_name="$(basename "$(dirname "$sock_path")")"
    [[ "$profile_name" != "$COLIMA_PROFILE" ]] && echo "unix://$sock_path" && return 0
  done
  return 1
}

main() {
  log_step "Stopping MCPJungle"

  local docker_host
  docker_host=$(find_host_docker_socket_stop 2>/dev/null) || {
    log_warn "No host Docker runtime found — MCPJungle may not be running"
    return 0
  }

  if ! DOCKER_HOST="$docker_host" docker compose -f "$COMPOSE_FILE" ps --services \
      --filter status=running 2>/dev/null | grep -q "mcpjungle"; then
    log_info "MCPJungle is not running"
    return 0
  fi

  DOCKER_HOST="$docker_host" docker compose -f "$COMPOSE_FILE" down --timeout 15
  log_success "MCPJungle stopped"
}

main "$@"
